package reviews

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

// nextMigrationName 返回紧接在 last 之后的迁移文件名，保持版本号连续——
// 迁移器要求版本从 0001 起不得有空洞。
func nextMigrationName(last string) string {
	version, err := strconv.Atoi(strings.SplitN(last, "_", 2)[0])
	if err != nil {
		panic("unexpected migration file name " + last)
	}
	return fmt.Sprintf("%04d_synthetic_probe.sql", version+1)
}

// 每次迁移都必须真的建出它声明的对象。此前这些断言分散在七个按历史版本钉死的
// 升级测试里，与「失败回滚」混在一起；那种结构随每次新增列而碎，见
// TestFailedMigrationRollsBackWithoutTouchingDataOrLedger 的说明。此处只做
// schema 盘点，不灌数据，因此不受应用代码写入哪些列的影响。
func TestMigrationsCreateTheirDeclaredObjects(t *testing.T) {
	ctx := context.Background()
	config := postgresqltest.NewDatabase(t)
	if err := postgres.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, table := range []string{
		"public.trip_evidence_facts",     // 0002
		"public.invoice_material_links",  // 0006
		"public.member_invitations",      // 0007
		"public.fact_bad_debt_decisions", // 0008
	} {
		var found *string
		if err := store.DB().QueryRow(`SELECT to_regclass(?)::text`, table).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found == nil {
			t.Fatalf("迁移未建出表 %s", table)
		}
	}

	for _, column := range []struct{ table, name string }{
		{"claim_sets", "manual_reason"},            // 0003
		{"payments", "current_review_decision_id"}, // 0004
		{"payments", "merchant_full_name"},         // 0009
	} {
		var count int
		if err := store.DB().QueryRow(
			`SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name=? AND column_name=?`,
			column.table, column.name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("迁移未建出列 %s.%s", column.table, column.name)
		}
	}

	var indexes int
	if err := store.DB().QueryRow(
		`SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND indexname IN ('payments_tenant_created_active_idx','invoices_tenant_created_active_idx')`,
	).Scan(&indexes); err != nil { // 0005
		t.Fatal(err)
	}
	if indexes != 2 {
		t.Fatalf("迁移未建出事实查询索引，实得 %d 个", indexes)
	}
}
