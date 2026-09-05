//go:build postgresql_tools

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func seedManualRestoreReview(t *testing.T, store *postgres.Store, objects *localstorage.Store, tenant domain.TenantContext) ports.ReviewSnapshot {
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
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	upload := documents.NewUploadService(objects, inspector, store, system.IDGenerator{}, system.Clock{})
	created, err := upload.Execute(ctx, documents.UploadInput{Tenant: tenant, Name: "synthetic-manual-restore.png", MIME: "image/png", Source: bytes.NewReader(content)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.LeaseNextJob(ctx, "restore-fixture", now, now.Add(165*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTransaction(ctx, func(tx ports.Transaction) error {
		return tx.MarkJobFailed(ctx, tenant.TenantID, created.JobID, "provider_config_missing", "合成人工恢复", now)
	}); err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(ctx, tenant.TenantID, created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	service := reviews.NewService(store, store, system.IDGenerator{}, system.Clock{}).WithManualEntry(store, normalizer, objects)
	if _, err := service.StartManualReview(ctx, tenant, created.JobID, reviews.ManualReviewInput{ExpectedJobVersion: job.Version, DocumentType: domain.DocumentPayment, Reason: "合成人工恢复验证", IdempotencyKey: "manual-restore-root"}); err != nil {
		t.Fatal(err)
	}
	root, err := service.Get(ctx, tenant, created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	revision := reviews.RevisionInput{ExpectedRevision: root.Revision, ExpectedOptimisticVersion: root.OptimisticVersion, DocumentType: domain.DocumentPayment}
	for _, field := range root.Fields {
		if field.Path == "document_type" {
			continue
		}
		entry := reviews.RevisionFieldInput{Path: field.Path, ValueType: field.ValueType, Presence: "absent"}
		if field.Path == "merchant" {
			entry.Presence = "present"
			entry.Value = json.RawMessage(`"合成恢复商户"`)
			entry.ManualEvidence = []domain.ManualEvidenceInput{{Page: 1, Quote: "合成原件摘录"}}
		}
		revision.Fields = append(revision.Fields, entry)
	}
	result, err := service.Revise(ctx, tenant, created.JobID, revision)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func seedCorrectedRestoreFact(t *testing.T, store *postgres.Store, tenant domain.TenantContext, current ports.ReviewSnapshot) reviews.CorrectionWorkspace {
	t.Helper()
	ctx := context.Background()
	service := reviews.NewService(store, store, system.IDGenerator{}, system.Clock{})
	input := reviews.RevisionInput{ExpectedRevision: current.Revision, ExpectedOptimisticVersion: current.OptimisticVersion, DocumentType: domain.DocumentPayment}
	values := map[string]any{"amount_minor": 12345, "currency": "CNY", "merchant": "合成恢复商户", "transaction_time": "2026-08-27T12:00:00+08:00", "source_timezone": "Asia/Shanghai"}
	for _, field := range current.Fields {
		if field.Path == "document_type" {
			continue
		}
		entry := reviews.RevisionFieldInput{Path: field.Path, ValueType: field.ValueType, Presence: "absent"}
		if value, exists := values[field.Path]; exists {
			entry.Presence = "present"
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			entry.Value = encoded
			entry.ManualEvidence = []domain.ManualEvidenceInput{{Page: 1, Quote: "合成完整恢复摘录"}}
		}
		input.Fields = append(input.Fields, entry)
	}
	ready, err := service.Revise(ctx, tenant, current.Job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := service.Confirm(ctx, tenant, current.Job.ID, reviews.ConfirmInput{ExpectedRevision: ready.Revision, AssociationMode: reviews.AssociationNoCandidate, IdempotencyKey: "restore-confirmed-fact", RequestID: "restore-confirmed-fact-request"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := service.GetCorrection(ctx, tenant, domain.DocumentPayment, confirmed.FactID)
	if err != nil {
		t.Fatal(err)
	}
	correction := reviews.CorrectionInput{ExpectedVersion: w.State.Version, CurrentReviewDecisionID: w.State.CurrentReviewDecisionID, Reason: "合成纠错恢复", WithdrawLinkIDs: []string{}}
	for _, field := range w.Review.Fields {
		if field.Path == "document_type" {
			continue
		}
		entry := reviews.RevisionFieldInput{Path: field.Path, ValueType: field.ValueType, Presence: field.Presence, Value: field.Value}
		if field.Path == "merchant" {
			entry.Value = json.RawMessage(`"合成更正后恢复商户"`)
			entry.ManualEvidence = []domain.ManualEvidenceInput{{Page: 1, Quote: "合成纠错恢复摘录"}}
		}
		correction.Fields = append(correction.Fields, entry)
	}
	preview, err := service.PreviewCorrection(ctx, tenant, domain.DocumentPayment, confirmed.FactID, correction)
	if err != nil || !preview.CanConfirm {
		t.Fatalf("restore correction preview: %v", err)
	}
	if _, err := service.ConfirmCorrection(ctx, tenant, domain.DocumentPayment, confirmed.FactID, reviews.CorrectionConfirmInput{CorrectionInput: correction, PreviewHash: preview.PreviewHash, AcknowledgedDuplicateKeys: []string{}, IdempotencyKey: "restore-correction", RequestID: "restore-correction-request"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.GetCorrection(ctx, tenant, domain.DocumentPayment, confirmed.FactID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
