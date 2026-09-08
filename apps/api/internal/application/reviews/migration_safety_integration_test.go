package reviews

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

// 迁移失败必须整体回滚：既不留下半截 schema，也不改动既有数据与账本。
//
// 此前由七个测试分别验证，每个钉死一段历史迁移（0001..N-1）再重放第 N 次迁移。
// 那种写法要求「当前应用代码写入的列在第 N-1 版就已存在」——种子数据只能用当前
// 代码创建，而迁移器要求版本号从 0001 连续、且没有 down 机制，因此任何一次新增
// 列都会让七个测试同时失败，且无法通过补迁移绕开。
//
// 改为在完整 schema 上灌入数据，再注入一次合成的失败迁移。被验证的不变量没有
// 变——迁移失败时数据与 schema 完好——而且不再随任何一次新增迁移而碎。
//
// 已知覆盖缺口：本测试不再验证「把第 N 次迁移应用到第 N 次之前就已存在的数据」。
// 那需要一份旧版本数据库的快照，不能用当前代码伪造；此前的写法同样是用今天的
// 代码在昨天的 schema 上造数据，只是恰好能写进去，并非真的旧数据。
func TestFailedMigrationRollsBackWithoutTouchingDataOrLedger(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	source := config.MigrationsDir
	config.MigrationsDir = t.TempDir()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	last := ""
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config.MigrationsDir, entry.Name()), content, 0600); err != nil {
			t.Fatal(err)
		}
		if entry.Name() > last {
			last = entry.Name()
		}
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	f := newReviewFixtureInStore(t, store)
	s := NewService(store, store, system.IDGenerator{}, fixedClock{now: f.now})
	r, err := s.Get(ctx, f.tenant, f.jobID)
	if err != nil {
		t.Fatal(err)
	}
	confirmFactWithoutLinks(t, s, f.tenant, r, "migration-safety-payment")
	confirmFactWithoutLinks(t, s, f.tenant,
		seedAdditionalReview(t, f, invoiceWithItemsEnvelope("MIGRATION-SAFETY"), "migration-safety-invoice"),
		"migration-safety-invoice")

	snapshot := func() string {
		t.Helper()
		var value string
		if err := store.DB().QueryRow(`SELECT jsonb_build_object('payments',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM payments p),'invoices',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM invoices i),'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM invoice_items i),'claims',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM claim_sets c),'origins',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM fact_field_origins o))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	ledger := func() int {
		t.Helper()
		var count int
		if err := store.DB().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	dataBefore, ledgerBefore := snapshot(), ledger()

	// 合成迁移建一张表后立即抛错，用来区分「事务整体回滚」与「留下半截 schema」。
	next := nextMigrationName(last)
	broken := "CREATE TABLE synthetic_migration_probe (id TEXT PRIMARY KEY);\n" +
		"DO $$ BEGIN RAISE EXCEPTION 'synthetic_migration_failure'; END; $$;\n"
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, next), []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err == nil {
		t.Fatal("injected migration failure unexpectedly succeeded")
	}
	var probe *string
	if err := store.DB().QueryRow(`SELECT to_regclass('public.synthetic_migration_probe')::text`).Scan(&probe); err != nil {
		t.Fatal(err)
	}
	if probe != nil {
		t.Fatal("failed migration left partial schema")
	}
	if ledger() != ledgerBefore {
		t.Fatal("failed migration was recorded in the ledger")
	}
	if snapshot() != dataBefore {
		t.Fatal("failed migration changed existing data")
	}

	// 修好同一次迁移后必须能继续前进，且既有数据不受影响。
	fixed := "CREATE TABLE synthetic_migration_probe (id TEXT PRIMARY KEY);\n"
	if err := os.WriteFile(filepath.Join(config.MigrationsDir, next), []byte(fixed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT to_regclass('public.synthetic_migration_probe')::text`).Scan(&probe); err != nil {
		t.Fatal(err)
	}
	if probe == nil {
		t.Fatal("repaired migration did not apply")
	}
	if ledger() != ledgerBefore+1 || snapshot() != dataBefore {
		t.Fatal("repaired migration did not preserve records")
	}
}
