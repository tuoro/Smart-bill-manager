//go:build postgresql_tools

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/invoicematerials"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const materialRestoreHistorySQL = `SELECT jsonb_build_object(
 'links',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM invoice_material_links l),
 'decisions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM invoice_material_decisions d),
 'snapshots',(SELECT jsonb_agg(to_jsonb(s) ORDER BY link_id) FROM reimbursement_material_snapshots s),
 'reimbursements',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM reimbursements r))::text`

func restoreMaterialPNG(t *testing.T, red uint8) []byte {
	t.Helper()
	var out bytes.Buffer
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.NRGBA{R: red, G: 30, B: 40, A: 255})
	if err := png.Encode(&out, picture); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func seedRestoreInvoice(t *testing.T, store *postgres.Store, objects *localstorage.Store, tenant domain.TenantContext, label string, red uint8) string {
	t.Helper()
	ctx := context.Background()
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := localstorage.NewNormalizer(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	upload := documents.NewUploadService(objects, inspector, store, system.IDGenerator{}, system.Clock{})
	created, err := upload.Execute(ctx, documents.UploadInput{Tenant: tenant, Name: label + ".png", MIME: "image/png", Source: bytes.NewReader(restoreMaterialPNG(t, red))})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.LeaseNextJob(ctx, "material-restore-fixture", now, now.Add(165*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTransaction(ctx, func(tx ports.Transaction) error {
		return tx.MarkJobFailed(ctx, tenant.TenantID, created.JobID, "provider_config_missing", "合成恢复", now)
	}); err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(ctx, tenant.TenantID, created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	s := reviews.NewService(store, store, system.IDGenerator{}, system.Clock{}).WithManualEntry(store, normalizer, objects)
	if _, err := s.StartManualReview(ctx, tenant, job.ID, reviews.ManualReviewInput{DocumentType: domain.DocumentInvoice, ExpectedJobVersion: job.Version, Reason: "合成发票恢复", IdempotencyKey: label + "-manual-root"}); err != nil {
		t.Fatal(err)
	}
	root, err := s.Get(ctx, tenant, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := reviews.RevisionInput{DocumentType: domain.DocumentInvoice, ExpectedRevision: root.Revision, ExpectedOptimisticVersion: root.OptimisticVersion}
	values := map[string]any{"invoice_number": label, "invoice_date": "2026-09-02", "total_minor": 1000, "currency": "CNY", "seller_name": "合成销售方", "buyer_name": "合成购买方"}
	for _, field := range root.Fields {
		if field.Path == "document_type" {
			continue
		}
		entry := reviews.RevisionFieldInput{Path: field.Path, ValueType: field.ValueType, Presence: "absent"}
		if value, ok := values[field.Path]; ok {
			entry.Presence = "present"
			entry.Value, err = json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			entry.ManualEvidence = []domain.ManualEvidenceInput{{Page: 1, Quote: "合成恢复摘录"}}
		}
		input.Fields = append(input.Fields, entry)
	}
	ready, err := s.Revise(ctx, tenant, job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	mode := reviews.AssociationNoCandidate
	if len(ready.Candidates) > 0 {
		mode = reviews.AssociationRejectAll
	}
	resolutions := []domain.DuplicateResolution{}
	for _, candidate := range ready.DuplicateCandidates {
		resolutions = append(resolutions, domain.DuplicateResolution{CandidateID: candidate.ID, Action: domain.DuplicateKeepDistinct})
	}
	fact, err := s.Confirm(ctx, tenant, job.ID, reviews.ConfirmInput{ExpectedRevision: ready.Revision, AssociationMode: mode, DuplicateResolutions: resolutions, IdempotencyKey: label + "-confirm", RequestID: label + "-request"})
	if err != nil {
		t.Fatal(err)
	}
	return fact.FactID
}

func seedInvoiceMaterialRestore(t *testing.T, store *postgres.Store, objects *localstorage.Store, tenant domain.TenantContext, tripID string) string {
	t.Helper()
	ctx := context.Background()
	first := seedRestoreInvoice(t, store, objects, tenant, "SYN-RESTORE-INV-A", 81)
	second := seedRestoreInvoice(t, store, objects, tenant, "SYN-RESTORE-INV-B", 82)
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	s := invoicematerials.NewService(store, store, objects, objects, inspector, system.IDGenerator{}, system.Clock{})
	w, err := s.Workspace(ctx, tenant, first)
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Upload(ctx, tenant, domain.InvoiceMaterialRequest{InvoiceID: first, Action: "upload", ExpectedVersion: w.Version, Reason: "合成材料恢复", IdempotencyKey: "restore-material-upload"}, documents.UploadInput{Name: "synthetic-shared-material.png", MIME: "image/png", Source: bytes.NewReader(restoreMaterialPNG(t, 83))}, "restore-material-request")
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.Workspace(ctx, tenant, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Change(ctx, tenant, domain.InvoiceMaterialRequest{InvoiceID: second, Action: "add", DocumentID: added.DocumentID, ExpectedVersion: w.Version, Reason: "合成共享材料", IdempotencyKey: "restore-material-share"}, "restore-share-request"); err != nil {
		t.Fatal(err)
	}
	tripService := trips.NewService(store, store, system.IDGenerator{}, system.Clock{})
	assignment, err := tripService.Assign(ctx, tenant, trips.AssignmentInput{FactType: domain.DocumentInvoice, FactID: first, ExpectedFactVersion: added.Version, DesiredTripID: &tripID, Reason: "合成报销归属", IdempotencyKey: "restore-material-assign", RequestID: "restore-assignment-request"})
	if err != nil {
		t.Fatal(err)
	}
	rs := reimbursements.NewService(store, store, system.IDGenerator{}, system.Clock{})
	selection := []string{assignment.AssignmentID}
	pre, err := rs.Preview(ctx, tenant, tripID, selection)
	if err != nil || len(pre.Materials) != 1 {
		t.Fatalf("restore material preview: %v", err)
	}
	if _, err := rs.Submit(ctx, tenant, reimbursements.SubmissionInput{TripID: tripID, AssignmentIDs: selection, ExpectedSnapshotHash: pre.SnapshotHash, AcknowledgedFindingKeys: []string{}, Reason: "合成报销材料固定", IdempotencyKey: "restore-material-submit", RequestID: "restore-submit-request"}); err != nil {
		t.Fatal(err)
	}
	w, err = s.Workspace(ctx, tenant, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Change(ctx, tenant, domain.InvoiceMaterialRequest{InvoiceID: first, Action: "remove", LinkID: added.LinkID, ExpectedVersion: w.Version, Reason: "合成解除后历史保留", IdempotencyKey: "restore-material-remove"}, "restore-remove-request"); err != nil {
		t.Fatal(err)
	}
	var history string
	if err := store.DB().QueryRow(materialRestoreHistorySQL).Scan(&history); err != nil {
		t.Fatal(err)
	}
	return history
}
