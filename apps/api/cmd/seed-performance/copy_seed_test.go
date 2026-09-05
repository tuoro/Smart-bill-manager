package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// 捕获造数器实际发送的行；完整 PostgreSQL 约束由 10,000 Fact 发布演练复验。
type revisionCheckingCopyTx struct {
	pgx.Tx
	t      *testing.T
	counts map[string]int64
}

func (tx *revisionCheckingCopyTx) CopyFrom(_ context.Context, table pgx.Identifier, columns []string, rows pgx.CopyFromSource) (int64, error) {
	var copied int64
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return copied, err
		}
		if len(columns) != len(values) {
			tx.t.Fatalf("%s column/value count mismatch", table[0])
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			row[column] = values[index]
		}
		switch table[0] {
		case "review_decisions":
			kind := "payment"
			if copied >= 5_000 {
				kind = "invoice"
			}
			if row["fact_type"] != kind || row["action"] != "confirm" {
				tx.t.Fatal("performance confirmation must explicitly bind its Fact type")
			}
		case "payments", "invoices":
			expected := strings.Replace(row["id"].(string), "60000001", "50000001", 1)
			if row["source_review_decision_id"] != expected || row["current_review_decision_id"] != expected {
				tx.t.Fatal("performance Fact must bind first and current confirmation")
			}
		case "invoice_items":
			expected := strings.Replace(row["invoice_id"].(string), "60000001", "50000001", 1)
			if row["review_decision_id"] != expected {
				tx.t.Fatal("performance invoice item must bind its confirmation revision")
			}
		}
		copied++
	}
	tx.counts[table[0]] += copied
	return copied, rows.Err()
}

func TestPerformanceSeedBindsCurrentSchemaReviewIdentity(t *testing.T) {
	tx := &revisionCheckingCopyTx{t: t, counts: make(map[string]int64)}
	if err := seedPerformanceData(context.Background(), tx, "tenant", "owner", "provider", time.Date(2026, 8, 28, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	for table, expected := range map[string]int64{
		"documents": 10_220, "claim_sets": 10_220, "review_decisions": 10_000,
		"payments": 5_000, "invoices": 5_000, "invoice_items": 5_000, "fact_field_origins": 85_000,
	} {
		if tx.counts[table] != expected {
			t.Fatalf("%s count = %d, want %d", table, tx.counts[table], expected)
		}
	}
}
