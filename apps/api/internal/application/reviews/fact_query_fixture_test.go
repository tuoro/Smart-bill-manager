package reviews

import (
	"context"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

// 合成批量记录经过真实 PostgreSQL 约束；需要来源更正的记录由调用方走完整审核链创建。
func seedFactPageBoundary(t *testing.T, f reviewFixture, kind domain.DocumentType, count int) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO documents
	 (id,tenant_id,storage_key,original_name,declared_mime,detected_mime,size_bytes,sha256,page_count,status,ingestion_kind,original_object_owner,created_by_user_id,created_at)
	 SELECT 'query-bulk-document-'||g,?,'tenants/'||?||'/documents/query-bulk-'||g||'.png',
	 'query-bulk-'||g||'.png','image/png','image/png',100,lpad(g::text,64,'0'),1,'completed','upload','document',?,?
	 FROM generate_series(1,?) AS g`, f.tenant.TenantID, f.tenant.TenantID, f.tenant.UserID, f.now, count)
	exec(`INSERT INTO claim_sets
	 (id,tenant_id,document_id,revised_by_user_id,document_type,status,revision,optimistic_version,created_at,manual_reason,manual_idempotency_key,manual_request_hash)
	 SELECT 'query-bulk-claim-'||g,?,'query-bulk-document-'||g,?,?,'confirmed',1,1,?,
	 '合成分页边界','query-bulk-manual-'||g,lpad(to_hex(g),64,'a')
	 FROM generate_series(1,?) AS g`, f.tenant.TenantID, f.tenant.UserID, string(kind), f.now, count)
	exec(`INSERT INTO review_decisions
	 (id,tenant_id,claim_set_id,actor_user_id,action,fact_type,association_mode,duplicate_plan_hash,idempotency_key,expected_revision,created_at)
	 SELECT 'query-bulk-review-'||g,?,'query-bulk-claim-'||g,?,'confirm',?,'no_candidate',lpad(to_hex(g),64,'b'),'query-bulk-confirm-'||g,1,?
	 FROM generate_series(1,?) AS g`, f.tenant.TenantID, f.tenant.UserID, string(kind), f.now, count)
	if kind == domain.DocumentPayment {
		exec(`INSERT INTO payments
		 (id,tenant_id,source_review_decision_id,amount_minor,currency,merchant,transaction_time,source_timezone,business_date,created_at,updated_at,version,current_review_decision_id)
		 SELECT '00000000-0000-4000-8000-'||lpad(g::text,12,'0'),?,'query-bulk-review-'||g,10000+g,'CNY','合成分页商户 '||g,
		 '2026-08-27T12:00:00+08:00','Asia/Shanghai','2026-08-27',?,?,1,'query-bulk-review-'||g
		 FROM generate_series(1,?) AS g`, f.tenant.TenantID, f.now, f.now, count)
	} else {
		exec(`INSERT INTO invoices
		 (id,tenant_id,source_review_decision_id,invoice_number,normalized_invoice_number,invoice_date,total_minor,currency,seller_name,buyer_name,created_at,updated_at,version,current_review_decision_id)
		 SELECT '00000000-0000-4000-8000-'||lpad(g::text,12,'0'),?,'query-bulk-review-'||g,'QUERY-BULK-'||g,'QUERY-BULK-'||g,
		 '2026-08-27',10000+g,'CNY','合成销售方','合成购买方',?,?,1,'query-bulk-review-'||g
		 FROM generate_series(1,?) AS g`, f.tenant.TenantID, f.now, f.now, count)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
