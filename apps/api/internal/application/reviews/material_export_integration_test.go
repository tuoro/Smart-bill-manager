package reviews

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	reimbursementapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestExportCurrentTripCompleteBeyondManagementPage(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	trip := seedManualTrip(t, f, "export-many", "合成完整清单", "2026-08-26", "2026-08-28")
	seedAssignedPaymentsForExportBoundary(t, f, trip.TripID, 201)
	inventory, err := f.store.BuildMaterialExport(ctx, f.tenant.TenantID, domain.ExportScope{Kind: "trip", ID: trip.TripID})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := domain.CanonicalExportManifest(inventory.Manifest)
	if err != nil || len(manifest.References) != 201 || len(manifest.Files) != 201 || len(inventory.StorageKeys) != 201 {
		t.Fatalf("full scope truncated: %v", err)
	}
	for _, reference := range manifest.References {
		if reference.ReviewDecisionID == nil || reference.FactVersion == nil || reference.Kind != "original" {
			t.Fatal("current version omitted")
		}
	}
	other := addTenantReviewFixture(t, f)
	if _, err := f.store.BuildMaterialExport(ctx, other.tenant.TenantID, manifest.Scope); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross tenant scope visible")
	}
}

// 这个测试只验证数据库导出查询不会复用管理页的 200 条上限。批量夹具仍经过
// PostgreSQL 的外键、唯一约束和归属触发器，但不重复执行 201 次完整 AI 审核工作流。
func seedAssignedPaymentsForExportBoundary(t *testing.T, fixture reviewFixture, tripID string, count int) {
	t.Helper()
	ctx := context.Background()
	tx, err := fixture.store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(statement string, arguments ...any) {
		t.Helper()
		if _, execErr := tx.ExecContext(ctx, statement, arguments...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO documents
		(id,tenant_id,storage_key,original_name,declared_mime,detected_mime,size_bytes,sha256,page_count,status,ingestion_kind,original_object_owner,created_by_user_id,created_at)
		SELECT 'export-bulk-document-'||lpad(g::text,3,'0'),?,
		 'tenants/'||?||'/documents/export-bulk-'||lpad(g::text,3,'0')||'.png',
		 'export-bulk-'||lpad(g::text,3,'0')||'.png','image/png','image/png',100,lpad(g::text,64,'0'),1,'completed','upload','document',?,?
		FROM generate_series(1,?) AS g`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.UserID, fixture.now, count)
	exec(`INSERT INTO claim_sets
		(id,tenant_id,document_id,origin_ai_run_id,produced_by_ai_run_id,revised_by_user_id,document_type,status,revision,supersedes_claim_set_id,optimistic_version,created_at,manual_reason,manual_idempotency_key,manual_request_hash)
		SELECT 'export-bulk-claim-'||lpad(g::text,3,'0'),?,
		 'export-bulk-document-'||lpad(g::text,3,'0'),NULL,NULL,?,'payment','confirmed',1,NULL,1,?,
		 '合成批量导出边界','export-bulk-manual-'||lpad(g::text,3,'0'),lpad(to_hex(g),64,'a')
		FROM generate_series(1,?) AS g`, fixture.tenant.TenantID, fixture.tenant.UserID, fixture.now, count)
	exec(`INSERT INTO review_decisions
		(id,tenant_id,claim_set_id,actor_user_id,action,fact_type,association_mode,association_plan_hash,duplicate_plan_hash,idempotency_key,expected_revision,reason,created_at)
		SELECT 'export-bulk-review-'||lpad(g::text,3,'0'),?,'export-bulk-claim-'||lpad(g::text,3,'0'),?,
		 'confirm','payment','no_candidate',NULL,lpad(to_hex(g),64,'b'),'export-bulk-confirm-'||lpad(g::text,3,'0'),1,NULL,?
		FROM generate_series(1,?) AS g`, fixture.tenant.TenantID, fixture.tenant.UserID, fixture.now, count)
	exec(`INSERT INTO payments
		(id,tenant_id,source_review_decision_id,amount_minor,currency,merchant,transaction_time,source_timezone,business_date,created_at,updated_at,version,current_review_decision_id)
		SELECT 'export-bulk-payment-'||lpad(g::text,3,'0'),?,'export-bulk-review-'||lpad(g::text,3,'0'),
		 10000+g,'CNY','合成批量商户 '||g,'2026-08-27T12:00:00+08:00','Asia/Shanghai','2026-08-27',?,?,1,
		 'export-bulk-review-'||lpad(g::text,3,'0')
		FROM generate_series(1,?) AS g`, fixture.tenant.TenantID, fixture.now, fixture.now, count)
	exec(`INSERT INTO audit_events
		(id,tenant_id,actor_user_id,action,resource_type,resource_id,request_id,safe_metadata_json,created_at)
		SELECT 'export-bulk-audit-'||lpad(g::text,3,'0'),?,?,'trip_fact_assigned','payment',
		 'export-bulk-payment-'||lpad(g::text,3,'0'),'export-bulk-assign-request-'||lpad(g::text,3,'0'),'{}'::jsonb,?
		FROM generate_series(1,?) AS g`, fixture.tenant.TenantID, fixture.tenant.UserID, fixture.now, count)
	exec(`INSERT INTO trip_fact_assignment_decisions
		(id,tenant_id,actor_user_id,fact_type,payment_id,invoice_id,previous_assignment_id,desired_trip_id,action,idempotency_key,request_hash,reason,audit_event_id,created_at,decision_source,expected_fact_version)
		SELECT 'export-bulk-assignment-decision-'||lpad(g::text,3,'0'),?,?, 'payment',
		 'export-bulk-payment-'||lpad(g::text,3,'0'),NULL,NULL,?,'assign',
		 'export-bulk-assign-'||lpad(g::text,3,'0'),lpad(to_hex(g),64,'c'),'合成批量导出归属',
		 'export-bulk-audit-'||lpad(g::text,3,'0'),?,'manual',1
		FROM generate_series(1,?) AS g`, fixture.tenant.TenantID, fixture.tenant.UserID, tripID, fixture.now, count)
	exec(`INSERT INTO trip_fact_assignments
		(id,tenant_id,trip_id,payment_id,invoice_id,created_by_decision_id,created_at)
		SELECT 'export-bulk-assignment-'||lpad(g::text,3,'0'),?,?,
		 'export-bulk-payment-'||lpad(g::text,3,'0'),NULL,
		 'export-bulk-assignment-decision-'||lpad(g::text,3,'0'),?
		FROM generate_series(1,?) AS g`, fixture.tenant.TenantID, tripID, fixture.now, count)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestExportFixedReimbursementDoesNotMixCurrentFactsOrMaterials(t *testing.T) {
	for _, status := range []domain.ReimbursementStatus{domain.ReimbursementStatusSubmitted, domain.ReimbursementStatusReimbursed, domain.ReimbursementStatusRejected} {
		t.Run(string(status), func(t *testing.T) {
			f := newInvoiceMaterialFixture(t)
			ctx := context.Background()
			s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			trip := seedManualTrip(t, f.reviewFixture, "export-scope", "合成材料交付", "2026-08-26", "2026-08-28")
			trips := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			first := f.upload(t, 701, "export-shared-material")
			second := f
			second.invoiceID = confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f.reviewFixture, invoiceEnvelope("SYN-EXPORT-SECOND"), "export-second"), "export-second-confirm").FactID
			second.change(t, "add", first.DocumentID, "export-second-shared")
			selection := []string{}
			assignments := map[string]string{}
			for index, id := range []string{f.invoiceID, second.invoiceID} {
				result, err := trips.Assign(ctx, f.tenant, tripapp.AssignmentInput{FactType: domain.DocumentInvoice, FactID: id, DesiredTripID: &trip.TripID, ExpectedFactVersion: assignmentVersion(t, f.reviewFixture, domain.DocumentInvoice, id), Reason: "合成交付范围", IdempotencyKey: fmt.Sprintf("export-assign-%d", index), RequestID: "export-assign"})
				if err != nil {
					t.Fatal(err)
				}
				selection = append(selection, result.AssignmentID)
				assignments[id] = result.AssignmentID
			}
			paymentReview, err := s.Get(ctx, f.tenant, f.jobID)
			if err != nil {
				t.Fatal(err)
			}
			confirmFactWithoutLinks(t, s, f.tenant, paymentReview, "export-unselected-payment")
			ticketReview := seedAdditionalReview(t, f.reviewFixture, tripEnvelope("合成出发地", "合成目的地", "2026-08-26", "2026-08-28"), "export-ticket")
			ticket, err := s.Confirm(ctx, f.tenant, ticketReview.Job.ID, ConfirmInput{ExpectedRevision: ticketReview.Revision, IdempotencyKey: "export-ticket-confirm", RequestID: "export-ticket-confirm"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := trips.AssignMaterial(ctx, f.tenant, tripapp.MaterialInput{EvidenceID: ticket.FactID, DesiredTripID: &trip.TripID, ExpectedVersion: 1, Reason: "合成机票", IdempotencyKey: "export-ticket-link", RequestID: "export-ticket-link"}); err != nil {
				t.Fatal(err)
			}
			current, err := f.store.BuildMaterialExport(ctx, f.tenant.TenantID, domain.ExportScope{Kind: "trip", ID: trip.TripID})
			if err != nil {
				t.Fatal(err)
			}
			if len(current.Manifest.References) != 6 || len(current.Manifest.Files) != 5 {
				t.Fatal("current scope omitted payment/ticket or duplicated shared document")
			}
			rs := reimbursementapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			preview, err := rs.Preview(ctx, f.tenant, trip.TripID, selection)
			if err != nil {
				t.Fatal(err)
			}
			created, err := rs.Submit(ctx, f.tenant, reimbursementapp.SubmissionInput{TripID: trip.TripID, AssignmentIDs: selection, ExpectedSnapshotHash: preview.SnapshotHash, AcknowledgedFindingKeys: reimbursementFindingKeysForTest(preview), Reason: "合成交付快照", IdempotencyKey: "export-submit", RequestID: "export-submit"})
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.ExportScope{Kind: "reimbursement", ID: created.ReimbursementID}
			submitted, err := f.store.BuildMaterialExport(ctx, f.tenant.TenantID, scope)
			if err != nil {
				t.Fatal(err)
			}
			submittedManifest, err := domain.CanonicalExportManifest(submitted.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			if status != domain.ReimbursementStatusSubmitted {
				if _, err := rs.ChangeStatus(ctx, f.tenant, created.ReimbursementID, reimbursementapp.StatusInput{ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: status, ExpectedVersion: 1, Reason: "合成状态", IdempotencyKey: "export-status", RequestID: "export-status"}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := f.store.BuildMaterialExport(ctx, f.tenant.TenantID, scope)
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := domain.CanonicalExportManifest(before.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			if baseline.Version != 1 || baseline.ManifestHash != submittedManifest.ManifestHash {
				t.Fatal("status transition changed fixed reimbursement snapshot identity")
			}
			if len(baseline.References) != 4 || len(baseline.Files) != 3 || len(baseline.Warnings) != 1 {
				t.Fatal("snapshot mixed unselected payment/current ticket or duplicated shared file")
			}
			for _, r := range baseline.References {
				if r.FactVersion != nil || r.ReviewDecisionID == nil {
					t.Fatal("snapshot revision fabricated or lost")
				}
			}
			workspace, err := s.GetCorrection(ctx, f.tenant, domain.DocumentInvoice, f.invoiceID)
			if err != nil {
				t.Fatal(err)
			}
			input := correctionInputFrom(workspace)
			correctionField(t, &input, "seller_name", "合成更正后销售方")
			applyCorrection(t, s, f.tenant, domain.DocumentInvoice, f.invoiceID, input, "export-later-correction")
			f.change(t, "remove", first.LinkID, "export-later-remove")
			f.upload(t, 702, "export-later-upload")
			other := seedManualTrip(t, f.reviewFixture, "export-other", "合成另一个行程", "2026-09-01", "2026-09-02")
			assignment := assignments[f.invoiceID]
			if _, err := trips.Assign(ctx, f.tenant, tripapp.AssignmentInput{FactType: domain.DocumentInvoice, FactID: f.invoiceID, DesiredTripID: &other.TripID, ExpectedAssignmentID: &assignment, ExpectedFactVersion: assignmentVersion(t, f.reviewFixture, domain.DocumentInvoice, f.invoiceID), Reason: "明确移动", IdempotencyKey: "export-move", RequestID: "export-move"}); err != nil {
				t.Fatal(err)
			}
			facts := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			if err := facts.Delete(ctx, f.tenant, domain.DocumentInvoice, f.invoiceID, "export-delete-invoice"); err != nil {
				t.Fatal(err)
			}
			deleteManualTrip(t, f.reviewFixture, trip, "export-delete-trip")
			after, err := f.store.BuildMaterialExport(ctx, f.tenant.TenantID, scope)
			if err != nil {
				t.Fatal(err)
			}
			final, err := domain.CanonicalExportManifest(after.Manifest)
			if err != nil || final.ManifestHash != baseline.ManifestHash {
				t.Fatalf("fixed snapshot changed after correction/move/delete/materials: %v", err)
			}
		})
	}
}
