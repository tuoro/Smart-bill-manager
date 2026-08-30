package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestInsightReadSnapshotStaysStableAcrossConcurrentCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := Open(ctx, Config{
		DatabasePath:  filepath.Join(t.TempDir(), "insight-snapshot.sqlite"),
		MigrationsDir: migrationsDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().ExecContext(ctx, `
		CREATE TABLE insight_snapshot_probe (id INTEGER PRIMARY KEY) STRICT;
		INSERT INTO insight_snapshot_probe (id) VALUES (1);
	`); err != nil {
		t.Fatal(err)
	}

	type readResult struct {
		first  int
		second int
		err    error
	}
	firstRead := make(chan struct{})
	writerDone := make(chan struct{})
	result := make(chan readResult, 1)
	go func() {
		counts, readErr := withInsightReadSnapshot(ctx, store.db, func(queryer insightQueryer) ([2]int, error) {
			var values [2]int
			if err := queryer.QueryRowContext(ctx, `SELECT count(*) FROM insight_snapshot_probe`).Scan(&values[0]); err != nil {
				return values, err
			}
			close(firstRead)
			select {
			case <-writerDone:
			case <-ctx.Done():
				return values, ctx.Err()
			}
			if err := queryer.QueryRowContext(ctx, `SELECT count(*) FROM insight_snapshot_probe`).Scan(&values[1]); err != nil {
				return values, err
			}
			return values, nil
		})
		result <- readResult{first: counts[0], second: counts[1], err: readErr}
	}()

	select {
	case <-firstRead:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO insight_snapshot_probe (id) VALUES (2)`); err != nil {
		t.Fatal(err)
	}
	close(writerDone)
	read := <-result
	if read.err != nil {
		t.Fatal(read.err)
	}
	if read.first != 1 || read.second != 1 {
		t.Fatalf("read snapshot changed across concurrent commit: %d -> %d", read.first, read.second)
	}
	var current int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM insight_snapshot_probe`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != 2 {
		t.Fatalf("concurrent commit was not visible after snapshot: %d", current)
	}
}

func TestFactInsightIndexesAreMigratedWithoutInsightTables(t *testing.T) {
	store := openTestStore(t)
	var indexes, tables int
	if err := store.DB().QueryRow(`
		SELECT
		  (SELECT count(*) FROM sqlite_master
		   WHERE type = 'index' AND name IN ('payments_tenant_business_date_active_idx', 'invoices_tenant_insight_active_idx')),
		  (SELECT count(*) FROM sqlite_master
		   WHERE type = 'table' AND lower(name) LIKE '%insight%')
	`).Scan(&indexes, &tables); err != nil {
		t.Fatal(err)
	}
	if indexes != 2 || tables != 0 {
		t.Fatalf("insight schema = indexes:%d tables:%d", indexes, tables)
	}
}

func TestInsightAllocationFilterCannotHideInvalidProjection(t *testing.T) {
	const tenantID = "synthetic-invalid-projection-tenant"
	database, err := sql.Open("sqlite", "file:fact-insight-invalid-projection?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close projection database: %v", err)
		}
	})
	createFactInsightProjectionSchema(t, database)
	if _, err := database.Exec(`
		INSERT INTO payments (id, tenant_id, business_date, merchant, amount_minor, currency)
		VALUES ('payment-invalid', ?, '2026-08-31', '合成非法投影', 100, 'CNY');
		INSERT INTO payment_invoice_links (tenant_id, payment_id, invoice_id, allocated_minor)
		VALUES (?, 'payment-invalid', 'invoice-invalid', 101);
	`, tenantID, tenantID); err != nil {
		t.Fatal(err)
	}
	filter, _, err := domain.CanonicalInsightFilter(domain.InsightFilter{
		AllocationStatus: domain.InsightStatusPartial,
	})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := queryInsightFacts(context.Background(), database, tenantID, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("invalid projection was hidden by allocation filter: %#v", facts)
	}
	if _, err := domain.BuildInsightPage(filter, facts, nil, 50); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("invalid projection error = %v", err)
	}
}

func BenchmarkFactInsightQueryTenThousandFacts(b *testing.B) {
	const tenantID = "synthetic-performance-tenant"
	database, err := sql.Open("sqlite", "file:fact-insight-performance?mode=memory&cache=shared")
	if err != nil {
		b.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	b.Cleanup(func() {
		if err := database.Close(); err != nil {
			b.Errorf("close benchmark database: %v", err)
		}
	})
	seedFactInsightBenchmark(b, database, tenantID)
	filter, _, err := domain.CanonicalInsightFilter(domain.InsightFilter{})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		facts, err := queryInsightFacts(ctx, database, tenantID, filter)
		if err != nil {
			b.Fatal(err)
		}
		page, err := domain.BuildInsightPage(filter, facts, nil, 100)
		if err != nil {
			b.Fatal(err)
		}
		if len(facts) != 10_000 || len(page.Groups) != 2 || len(page.Items) != 100 || page.Next == nil {
			b.Fatalf("unexpected benchmark result: facts=%d groups=%d items=%d next=%v", len(facts), len(page.Groups), len(page.Items), page.Next != nil)
		}
	}
}

func seedFactInsightBenchmark(b *testing.B, database *sql.DB, tenantID string) {
	b.Helper()
	ctx := context.Background()
	createFactInsightProjectionSchema(b, database)
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	payment, err := tx.PrepareContext(ctx, `
		INSERT INTO payments (id, tenant_id, business_date, merchant, amount_minor, currency)
		VALUES (?, ?, ?, ?, ?, 'CNY')
	`)
	if err != nil {
		_ = tx.Rollback()
		b.Fatal(err)
	}
	defer payment.Close()
	invoice, err := tx.PrepareContext(ctx, `
		INSERT INTO invoices (id, tenant_id, invoice_date, seller_name, total_minor, currency)
		VALUES (?, ?, ?, ?, ?, 'CNY')
	`)
	if err != nil {
		_ = tx.Rollback()
		b.Fatal(err)
	}
	defer invoice.Close()
	for index := 0; index < 5_000; index++ {
		date := fmt.Sprintf("2026-08-%02d", index%28+1)
		if _, err := payment.ExecContext(ctx, fmt.Sprintf("payment-%05d", index), tenantID, date, "合成商户", index+1); err != nil {
			_ = tx.Rollback()
			b.Fatal(err)
		}
		if _, err := invoice.ExecContext(ctx, fmt.Sprintf("invoice-%05d", index), tenantID, date, "合成销售方", index+1); err != nil {
			_ = tx.Rollback()
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}

func createFactInsightProjectionSchema(tb testing.TB, database *sql.DB) {
	tb.Helper()
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE payments (
			id TEXT NOT NULL, tenant_id TEXT NOT NULL, business_date TEXT NOT NULL,
			merchant TEXT NOT NULL, amount_minor INTEGER NOT NULL, currency TEXT NOT NULL,
			deleted_at TEXT
		);
		CREATE TABLE invoices (
			id TEXT NOT NULL, tenant_id TEXT NOT NULL, invoice_date TEXT NOT NULL,
			seller_name TEXT NOT NULL, total_minor INTEGER NOT NULL, currency TEXT NOT NULL,
			deleted_at TEXT
		);
		CREATE TABLE payment_invoice_links (
			tenant_id TEXT NOT NULL, payment_id TEXT NOT NULL, invoice_id TEXT NOT NULL,
			allocated_minor INTEGER NOT NULL, ended_at TEXT
		);
		CREATE TABLE trips (
			id TEXT NOT NULL, tenant_id TEXT NOT NULL, destination TEXT NOT NULL,
			start_date TEXT NOT NULL, end_date TEXT NOT NULL, deleted_at TEXT
		);
		CREATE TABLE trip_fact_assignments (
			tenant_id TEXT NOT NULL, payment_id TEXT, invoice_id TEXT,
			trip_id TEXT NOT NULL, ended_at TEXT
		);
		CREATE INDEX payments_tenant_business_date_active_idx
		ON payments (tenant_id, business_date DESC, currency, id DESC)
		WHERE deleted_at IS NULL;
		CREATE INDEX invoices_tenant_insight_active_idx
		ON invoices (tenant_id, invoice_date DESC, currency, id DESC)
		WHERE deleted_at IS NULL;
	`); err != nil {
		tb.Fatal(err)
	}
}
