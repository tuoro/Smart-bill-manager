package postgresqladapter

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestVisualDuplicateBandLookupIsTenantScopedAndKeepsWholeDocumentPriority(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	owner := ports.BootstrapOwner{
		UserID: "owner", TenantID: "tenant-a", Email: "owner@example.test", PasswordHash: "test-only",
		DisplayName: "Owner", TenantName: "Tenant A", DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: now,
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at)
		VALUES ('tenant-b', 'Tenant B', 'CNY', 'UTC', ?, ?)
	`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES ('tenant-b', 'owner', 'owner', 'active', ?, ?)
	`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	documents := []struct {
		tenantID string
		id       string
		hashes   [][2]uint64
	}{
		{tenantID: "tenant-a", id: "current", hashes: [][2]uint64{{0, 0}, {^uint64(0), ^uint64(0)}}},
		{tenantID: "tenant-a", id: "near", hashes: [][2]uint64{{1, 1}, {^uint64(0) - 1, ^uint64(0) - 1}}},
		{tenantID: "tenant-a", id: "partial", hashes: [][2]uint64{{2, 2}, {0xaaaaaaaaaaaaaaaa, 0xaaaaaaaaaaaaaaaa}}},
		{tenantID: "tenant-b", id: "foreign", hashes: [][2]uint64{{0, 0}, {^uint64(0), ^uint64(0)}}},
	}
	if err := store.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		for documentIndex, document := range documents {
			if err := transaction.InsertDocument(ctx, ports.Document{
				ID: document.id, TenantID: document.tenantID,
				StorageKey:   "tenants/" + document.tenantID + "/" + document.id,
				OriginalName: document.id + ".png", DeclaredMIME: "image/png", DetectedMIME: "image/png",
				SizeBytes: 100, SHA256: strings.Repeat(string("abcdef"[documentIndex]), 64),
				PageCount: len(document.hashes), Status: "stored",
				IngestionKind: domain.DocumentIngestionUpload, OriginalObjectOwner: domain.DocumentObjectOwnerDocument,
				CreatedByUserID: owner.UserID, CreatedAt: now,
			}); err != nil {
				return err
			}
			pages := make([]ports.DocumentPageRecord, 0, len(document.hashes))
			for pageIndex, hashes := range document.hashes {
				pages = append(pages, ports.DocumentPageRecord{
					ID: document.id + "-page-" + string(rune('1'+pageIndex)), TenantID: document.tenantID,
					DocumentID: document.id, PageNumber: pageIndex + 1,
					StorageKey: "tenants/" + document.tenantID + "/" + document.id + "/page-" + string(rune('1'+pageIndex)),
					Width:      1200, Height: 1800, SHA256: strings.Repeat(string("fedcba"[documentIndex]), 64),
					ProcessingVersion: "document-normalize/2",
					VisualFingerprint: domain.NewPageVisualFingerprint(hashes[0], hashes[1]),
					CreatedAt:         now,
				})
			}
			if err := transaction.InsertDocumentPages(ctx, pages); err != nil {
				return err
			}
		}
		current, targets, err := transaction.ListVisualDuplicateDocuments(ctx, "tenant-a", "current")
		if err != nil {
			return err
		}
		if len(targets) != 2 || targets[0].ID != "near" || targets[1].ID != "partial" {
			t.Fatalf("visual targets = %#v", targets)
		}
		signals, err := domain.BuildVisualDuplicateSignals(current, targets)
		if err != nil {
			return err
		}
		if len(signals) != 2 || signals[0].Kind != "near_file" ||
			signals[0].ExistingDocumentID != "near" || signals[1].Kind != "cross_page" ||
			signals[1].ExistingDocumentID != "partial" {
			t.Fatalf("visual signals = %#v", signals)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestVisualDuplicateBandLookupHasEachCompositeIndexAndAvoidsSequentialCandidateScan(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	for band := 0; band < 4; band++ {
		indexName := fmt.Sprintf("document_pages_visual_band_%d_idx", band)
		var definition string
		if err := store.DB().QueryRowContext(ctx, `
			SELECT indexdef FROM pg_indexes
			WHERE schemaname = 'public' AND indexname = ?
		`, indexName).Scan(&definition); err != nil {
			t.Fatalf("read %s: %v", indexName, err)
		}
		if !strings.Contains(definition, "(tenant_id, visual_fingerprint_version, dhash_band_"+fmt.Sprint(band)+")") {
			t.Fatalf("unexpected %s definition: %s", indexName, definition)
		}
	}
	transaction, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := transaction.QueryContext(
		ctx,
		"EXPLAIN (COSTS OFF) "+listVisualDuplicateDocumentsQuery,
		"tenant-a",
		"current",
		"tenant-a",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "document_pages_visual_band_") {
		t.Fatalf("visual duplicate query does not use a visual-band index:\n%s", plan.String())
	}
	if strings.Contains(plan.String(), "Seq Scan on document_pages target") {
		t.Fatalf("visual duplicate query scans every target page:\n%s", plan.String())
	}
}
