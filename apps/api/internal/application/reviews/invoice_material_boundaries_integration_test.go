package reviews

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/invoicematerials"
	reimbursementapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	tripapp "github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestInvoiceMaterialCandidateKeysetAndLimit(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	var documentIDs []string
	for index := 0; index < 201; index++ {
		staged, err := f.objects.Stage(ctx, bytes.NewReader(materialPNG(t, index+100)), ports.MaxDocumentBytes)
		if err != nil {
			t.Fatal(err)
		}
		id := mustID(t, system.IDGenerator{})
		key := "tenants/" + f.tenant.TenantID + "/documents/" + id + "/original"
		if err := f.objects.Commit(ctx, staged, key); err != nil {
			t.Fatal(err)
		}
		document := ports.Document{ID: id, TenantID: f.tenant.TenantID, StorageKey: key, OriginalName: fmt.Sprintf("page-%%_\\-%03d.png", index), DeclaredMIME: "image/png", DetectedMIME: "image/png", SizeBytes: staged.Size, SHA256: staged.SHA256, PageCount: 1, Status: "completed", IngestionKind: domain.DocumentIngestionUpload, OriginalObjectOwner: domain.DocumentObjectOwnerDocument, CreatedByUserID: f.tenant.UserID, CreatedAt: f.now}
		if err := f.store.WithinReadCommittedTransaction(ctx, func(tx ports.Transaction) error { return tx.InsertDocument(ctx, document) }); err != nil {
			t.Fatal(err)
		}
		documentIDs = append(documentIDs, id)
	}
	seen := map[string]bool{}
	cursor := ""
	firstCursor := ""
	for {
		page, err := f.service.Candidates(ctx, f.tenant, f.invoiceID, invoicematerials.CandidateQuery{Query: `%_\`, Cursor: cursor, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.DocumentID] {
				t.Fatal("duplicate candidate")
			}
			seen[item.DocumentID] = true
		}
		if firstCursor == "" {
			firstCursor = page.NextCursor
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 201 {
		t.Fatalf("candidate count %d", len(seen))
	}
	for _, q := range []invoicematerials.CandidateQuery{{Limit: 0}, {Limit: 101}, {Limit: 20, Cursor: "bad"}, {Limit: 20, Cursor: firstCursor, Query: "other"}} {
		if _, err := f.service.Candidates(ctx, f.tenant, f.invoiceID, q); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatal("invalid candidate range accepted")
		}
	}
	if _, err := f.service.Candidates(ctx, f.tenant, "other-invoice", invoicematerials.CandidateQuery{Limit: 20, Cursor: firstCursor, Query: `%_\`}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("cursor crossed invoice")
	}
	for index, id := range documentIDs[:100] {
		f.change(t, "add", id, fmt.Sprintf("material-limit-%03d", index))
	}
	input := f.input(t, "add", "material-limit-overflow")
	input.DocumentID = documentIDs[100]
	if _, err := f.service.Change(ctx, f.tenant, input, "limit"); !hasRuleCode(err, "invoice_material_limit") {
		t.Fatalf("limit not rejected: %v", err)
	}
	w, err := f.service.Workspace(ctx, f.tenant, f.invoiceID)
	if err != nil || len(w.Items) != 100 {
		t.Fatal("workspace truncated")
	}
}

func TestInvoiceMaterialReimbursementCaptureAndABAHistory(t *testing.T) {
	for _, status := range []domain.ReimbursementStatus{domain.ReimbursementStatusSubmitted, domain.ReimbursementStatusReimbursed, domain.ReimbursementStatusRejected} {
		t.Run(string(status), func(t *testing.T) {
			f := newInvoiceMaterialFixture(t)
			ctx := context.Background()
			trip := seedManualTrip(t, f.reviewFixture, "material-reimbursement-trip", "合成材料行程", "2026-08-26", "2026-08-28")
			trips := tripapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			assignment, err := trips.Assign(ctx, f.tenant, tripapp.AssignmentInput{FactType: domain.DocumentInvoice, FactID: f.invoiceID, DesiredTripID: &trip.TripID, ExpectedFactVersion: assignmentVersion(t, f.reviewFixture, domain.DocumentInvoice, f.invoiceID), Reason: "合成材料归属", IdempotencyKey: "material-reimbursement-assign", RequestID: "material-assign-request"})
			if err != nil {
				t.Fatal(err)
			}
			rs := reimbursementapp.NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
			selection := []string{assignment.AssignmentID}
			empty, err := rs.Preview(ctx, f.tenant, trip.TripID, selection)
			if err != nil || len(empty.Materials) != 0 {
				t.Fatal("empty materials not explicit")
			}
			first := f.upload(t, 400, "material-capture-upload")
			pre, err := rs.Preview(ctx, f.tenant, trip.TripID, selection)
			if err != nil || len(pre.Materials) != 1 || pre.Materials[0].LinkID != first.LinkID || pre.SnapshotHash == empty.SnapshotHash {
				t.Fatal("material not bound to preview")
			}
			submit := func(hash, key string) (ports.ReimbursementMutationResult, error) {
				return rs.Submit(ctx, f.tenant, reimbursementapp.SubmissionInput{TripID: trip.TripID, AssignmentIDs: selection, ExpectedSnapshotHash: hash, AcknowledgedFindingKeys: []string{}, Reason: "合成固定材料", IdempotencyKey: key, RequestID: key})
			}
			if _, err := submit(empty.SnapshotHash, "material-stale-empty"); !hasRuleCode(err, "reimbursement_snapshot_stale") {
				t.Fatal("added material did not stale preview")
			}
			created, err := submit(pre.SnapshotHash, "material-capture-submit")
			if err != nil {
				t.Fatal(err)
			}
			if status != domain.ReimbursementStatusSubmitted {
				if _, err := rs.ChangeStatus(ctx, f.tenant, created.ReimbursementID, reimbursementapp.StatusInput{ExpectedStatus: domain.ReimbursementStatusSubmitted, DesiredStatus: status, ExpectedVersion: 1, Reason: "合成状态", IdempotencyKey: "material-capture-status", RequestID: "material-status"}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := rs.Preview(ctx, f.tenant, trip.TripID, selection)
			if err != nil {
				t.Fatal(err)
			}
			f.change(t, "remove", first.LinkID, "material-capture-remove")
			readded := f.change(t, "add", first.DocumentID, "material-capture-readd")
			after, err := rs.Preview(ctx, f.tenant, trip.TripID, selection)
			if err != nil || before.SnapshotHash == after.SnapshotHash || after.Materials[0].LinkID != readded.LinkID {
				t.Fatal("ABA not bound")
			}
			detail, err := rs.Get(ctx, f.tenant, created.ReimbursementID)
			if err != nil || detail.Status != status || detail.SnapshotHash != pre.SnapshotHash || !detail.MaterialsCaptured || detail.MaterialCount == nil || *detail.MaterialCount != 1 {
				t.Fatal("material snapshot changed")
			}
			var retained string
			if err := f.store.DB().QueryRow(`SELECT link_id FROM reimbursement_material_snapshots WHERE reimbursement_id=?`, created.ReimbursementID).Scan(&retained); err != nil || retained != first.LinkID {
				t.Fatal("history replaced with current material")
			}
			viewer := f.tenant
			viewer.Role = domain.RoleViewer
			public, err := rs.Get(ctx, viewer, created.ReimbursementID)
			if err != nil || public.MaterialCount != nil || !public.MaterialsCaptured {
				t.Fatal("material count permission")
			}
			if _, err := f.store.DB().Exec(`UPDATE reimbursements SET material_count=2 WHERE id=?`, created.ReimbursementID); err == nil {
				t.Fatal("snapshot count mutable")
			}
			if _, err := f.store.DB().Exec(`INSERT INTO reimbursement_material_snapshots (tenant_id,reimbursement_id,invoice_id,link_id,document_id) VALUES (?,?,?,?,?)`, f.tenant.TenantID, created.ReimbursementID, f.invoiceID, readded.LinkID, readded.DocumentID); err == nil {
				t.Fatal("snapshot accepted late append")
			}
			if _, err := f.store.DB().Exec(`DELETE FROM reimbursement_material_snapshots WHERE reimbursement_id=?`, created.ReimbursementID); err == nil {
				t.Fatal("snapshot deletable")
			}
		})
	}
}

func TestInvoiceMaterialPublicationWaitsForCommitAndPreservesAdoptedObject(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	staged, err := f.objects.Stage(ctx, bytes.NewReader(materialPNG(t, 600)), ports.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	id := mustID(t, system.IDGenerator{})
	p := ports.MaterialPublication{ID: mustID(t, system.IDGenerator{}), TenantID: f.tenant.TenantID, DocumentID: id, StorageKey: "tenants/" + f.tenant.TenantID + "/documents/" + id + "/original", Staged: staged}
	locked := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- f.store.WithinReadCommittedTransaction(ctx, func(tx ports.Transaction) error {
			if err := tx.LockMaterialPublication(ctx, p.ID); err != nil {
				return err
			}
			if err := f.objects.RecordMaterialPublication(ctx, p); err != nil {
				return err
			}
			if err := f.objects.Commit(ctx, p.Staged, p.StorageKey); err != nil {
				return err
			}
			if err := tx.InsertDocument(ctx, ports.Document{ID: id, TenantID: f.tenant.TenantID, StorageKey: p.StorageKey, OriginalName: "synthetic-adopted.png", DeclaredMIME: "image/png", DetectedMIME: "image/png", SizeBytes: staged.Size, SHA256: staged.SHA256, PageCount: 1, Status: "stored", IngestionKind: domain.DocumentIngestionUpload, OriginalObjectOwner: domain.DocumentObjectOwnerDocument, CreatedByUserID: f.tenant.UserID, CreatedAt: f.now}); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-result:
		t.Fatalf("publication not locked: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("publication lock timeout")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	err = f.service.Reconcile(waitCtx)
	cancel()
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("recovery did not wait for final commit: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := f.service.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	body, err := f.objects.Open(ctx, p.StorageKey)
	if err != nil {
		t.Fatal("adopted object removed")
	}
	body.Close()
	f.assertPublicationEmpty(t)
}

func TestInvoiceMaterialDecisionMustCommitItsRelationshipEffect(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	added := f.upload(t, 601, "material-effect-first")
	tx, err := f.store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	audit, id := mustID(t, system.IDGenerator{}), mustID(t, system.IDGenerator{})
	if _, err := tx.Exec(`INSERT INTO audit_events(id,tenant_id,actor_user_id,action,resource_type,resource_id,request_id,safe_metadata_json,created_at) VALUES(?,?,?,'invoice_material_add','invoice',?,'synthetic-effect','{}',?)`, audit, f.tenant.TenantID, f.tenant.UserID, f.invoiceID, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO invoice_material_decisions(id,tenant_id,invoice_id,document_id,link_id,actor_user_id,action,expected_version,resulting_version,reason,idempotency_key,request_hash,audit_event_id,created_at) VALUES(?,?,?,?,?,?,'add',?,?,'合成无效果决定','material-effect-invalid',?,?,?)`, id, f.tenant.TenantID, f.invoiceID, added.DocumentID, added.LinkID, f.tenant.UserID, added.Version, added.Version+1, strings.Repeat("a", 64), audit, f.now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil || !strings.Contains(err.Error(), "invoice_material_decision_effect_mismatch") {
		t.Fatalf("ineffective decision committed: %v", err)
	}
}
