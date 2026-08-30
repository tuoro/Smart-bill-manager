package sqliteadapter

import (
	"context"
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
