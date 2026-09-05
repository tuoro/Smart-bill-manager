package localstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestMaterialPublicationPersistsExactIdentityWithoutPublishingBytes(t *testing.T) {
	ctx := context.Background()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		staged, err := store.Stage(ctx, strings.NewReader("synthetic material"), 100)
		if err != nil {
			t.Fatal(err)
		}
		id, err := store.ids.NewID()
		if err != nil {
			t.Fatal(err)
		}
		doc, err := store.ids.NewID()
		if err != nil {
			t.Fatal(err)
		}
		p := ports.MaterialPublication{ID: id, TenantID: "synthetic-tenant", DocumentID: doc, StorageKey: "tenants/synthetic-tenant/documents/" + doc + "/original", Staged: staged}
		if err := store.RecordMaterialPublication(ctx, p); err != nil {
			t.Fatal(err)
		}
		actual, err := store.GetMaterialPublication(ctx, id)
		if err != nil || actual != p {
			t.Fatal("publication identity not preserved")
		}
		if _, err := store.Open(ctx, p.StorageKey); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("journal published bytes")
		}
		if err := store.RecordMaterialPublication(ctx, p); !errors.Is(err, domain.ErrConflict) {
			t.Fatal("existing publication overwritten")
		}
	}
	pending, err := store.PendingMaterialPublications(ctx, 2)
	if err != nil || len(pending) != 2 {
		t.Fatal("publication inventory not bounded")
	}
	for _, p := range pending {
		if err := store.FinishMaterialPublication(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	pending, err = store.PendingMaterialPublications(ctx, 2)
	if err != nil || len(pending) != 1 {
		t.Fatal("publication cleanup mismatch")
	}
	p := pending[0]
	if err := store.Commit(ctx, p.Staged, p.StorageKey); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishMaterialPublication(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	body, err := store.Open(ctx, p.StorageKey)
	if err != nil {
		t.Fatal("finishing journal removed original")
	}
	body.Close()
	if err := store.FinishMaterialPublication(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
}

func TestMaterialPublicationRejectsInvalidOrChangedJournal(t *testing.T) {
	ctx := context.Background()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMaterialPublication(ctx, ports.MaterialPublication{}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("invalid publication accepted")
	}
	id, err := store.ids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, publicationDirectory, id+".json")
	if err := os.WriteFile(path, []byte(`{"id":"unknown","extra":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PendingMaterialPublications(ctx, 100); err == nil {
		t.Fatal("damaged journal accepted")
	}
	if _, err := store.PendingMaterialPublications(ctx, 101); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("unbounded scan accepted")
	}
}
