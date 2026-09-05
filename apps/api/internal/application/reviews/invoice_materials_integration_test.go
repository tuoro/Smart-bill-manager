package reviews

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/invoicematerials"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type invoiceMaterialFixture struct {
	reviewFixture
	objects         *localstorage.Store
	inspector       localstorage.Inspector
	service         invoicematerials.Service
	invoiceID, root string
}

func newInvoiceMaterialFixture(t *testing.T) invoiceMaterialFixture {
	t.Helper()
	f := newReviewFixture(t)
	root := t.TempDir()
	objects, err := localstorage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	invoice := confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f, invoiceEnvelope("SYN-MATERIAL-001"), "material-base"), "material-base-confirm")
	return invoiceMaterialFixture{f, objects, inspector, invoicematerials.NewService(f.store, f.store, objects, objects, inspector, system.IDGenerator{}, fixedClock{now: f.now}), invoice.FactID, root}
}

func materialPNG(t *testing.T, index int) []byte {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.NRGBA{R: uint8(index), G: uint8(index >> 8), B: 100, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func (f invoiceMaterialFixture) input(t *testing.T, action, key string) domain.InvoiceMaterialRequest {
	t.Helper()
	w, err := f.service.Workspace(context.Background(), f.tenant, f.invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	return domain.InvoiceMaterialRequest{InvoiceID: f.invoiceID, Action: action, ExpectedVersion: w.Version, Reason: "合成材料操作", IdempotencyKey: key}
}
func (f invoiceMaterialFixture) upload(t *testing.T, index int, key string) ports.InvoiceMaterialResult {
	t.Helper()
	result, err := f.service.Upload(context.Background(), f.tenant, f.input(t, "upload", key), documents.UploadInput{Name: "synthetic-material.png", MIME: "image/png", Source: bytes.NewReader(materialPNG(t, index))}, key)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func (f invoiceMaterialFixture) change(t *testing.T, action, target, key string) ports.InvoiceMaterialResult {
	t.Helper()
	input := f.input(t, action, key)
	if action == "add" {
		input.DocumentID = target
	} else {
		input.LinkID = target
	}
	result, err := f.service.Change(context.Background(), f.tenant, input, key)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func (f invoiceMaterialFixture) assertPublicationEmpty(t *testing.T) {
	t.Helper()
	for _, dir := range []string{"staging", "material-publications", "trash"} {
		entries, err := os.ReadDir(filepath.Join(f.root, dir))
		if err != nil || len(entries) != 0 {
			t.Fatalf("unfinished %s: %v", dir, err)
		}
	}
}

func TestInvoiceMaterialUploadReuseAndHistoryWithoutAI(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	var beforeJobs, beforeClaims, beforeRuns int
	const counts = `SELECT (SELECT count(*) FROM processing_jobs),(SELECT count(*) FROM claim_sets),(SELECT count(*) FROM ai_runs)`
	if err := f.store.DB().QueryRow(counts).Scan(&beforeJobs, &beforeClaims, &beforeRuns); err != nil {
		t.Fatal(err)
	}
	input := f.input(t, "upload", "material-first-upload")
	file := func(name string) documents.UploadInput {
		return documents.UploadInput{Name: name, MIME: "image/png", Source: bytes.NewReader(materialPNG(t, 1))}
	}
	first, err := f.service.Upload(ctx, f.tenant, input, file("first.png"), "first-request")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.service.Upload(ctx, f.tenant, input, file("first.png"), "retry-request")
	if err != nil || !replay.Replayed || replay.DocumentID != first.DocumentID || replay.LinkID != first.LinkID {
		t.Fatalf("upload replay: %v", err)
	}
	if _, err := f.service.Upload(ctx, f.tenant, input, file("changed.png"), "changed-request"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed payload replay: %v", err)
	}
	secondInvoice := f
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	secondInvoice.invoiceID = confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f.reviewFixture, invoiceEnvelope("SYN-MATERIAL-002"), "second-material-invoice"), "second-material-confirm").FactID
	shared, err := f.service.Upload(ctx, f.tenant, secondInvoice.input(t, "upload", "material-shared-upload"), file("renamed.png"), "shared-request")
	if err != nil || shared.DocumentID != first.DocumentID {
		t.Fatalf("same SHA not shared: %v", err)
	}
	w, err := secondInvoice.service.Workspace(ctx, f.tenant, secondInvoice.invoiceID)
	if err != nil || len(w.Items) != 1 || w.Items[0].OriginalName != "first.png" {
		t.Fatal("shared file identity replaced")
	}
	f.change(t, "remove", first.LinkID, "material-remove-first")
	added := f.change(t, "add", first.DocumentID, "material-readd-first")
	if added.LinkID == first.LinkID {
		t.Fatal("removed link reused")
	}
	var history, docs int
	if err := f.store.DB().QueryRow(`SELECT (SELECT count(*) FROM invoice_material_links),(SELECT count(*) FROM documents WHERE id=?)`, first.DocumentID).Scan(&history, &docs); err != nil || history != 3 || docs != 1 {
		t.Fatal("material history missing")
	}
	if _, err := f.store.DB().Exec(`UPDATE invoice_material_decisions SET reason='changed' WHERE link_id=?`, first.LinkID); err == nil {
		t.Fatal("decision mutable")
	}
	if _, err := f.store.DB().Exec(`DELETE FROM invoice_material_links WHERE id=?`, first.LinkID); err == nil {
		t.Fatal("history deleted")
	}
	// 第二张合成发票新增一个 Job、Claim、AiRun；三次材料写入不新增它们。
	var jobs, claims, runs int
	if err := f.store.DB().QueryRow(counts).Scan(&jobs, &claims, &runs); err != nil || jobs != beforeJobs+1 || claims != beforeClaims+1 || runs != beforeRuns+1 {
		t.Fatal("auxiliary upload created AI state")
	}
	f.assertPublicationEmpty(t)
}

func TestInvoiceMaterialExistingDocumentsIgnoreRecognitionStatus(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	for index, status := range []string{"stored", "processing", "needs_review", "completed", "failed"} {
		upload := documents.NewUploadService(f.objects, f.inspector, f.store, system.IDGenerator{}, fixedClock{now: f.now})
		created, err := upload.Execute(ctx, documents.UploadInput{Tenant: f.tenant, Name: status + ".png", MIME: "image/png", Source: bytes.NewReader(materialPNG(t, index+20))})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.DB().Exec(`UPDATE documents SET status=? WHERE id=?`, status, created.DocumentID); err != nil {
			t.Fatal(err)
		}
		page, err := f.service.Candidates(ctx, f.tenant, f.invoiceID, invoicematerials.CandidateQuery{Query: status + ".png", Limit: 20})
		if err != nil || len(page.Items) != 1 || page.Items[0].DocumentID != created.DocumentID {
			t.Fatalf("candidate status %s: %v", status, err)
		}
		added := f.change(t, "add", created.DocumentID, "material-status-add-"+status)
		f.change(t, "remove", added.LinkID, "material-status-remove-"+status)
		if _, err := f.service.Upload(ctx, f.tenant, f.input(t, "upload", "material-status-upload-"+status), documents.UploadInput{Name: "same.png", MIME: "image/png", Source: bytes.NewReader(materialPNG(t, index+20))}, "status-upload"); err != nil {
			t.Fatal(err)
		}
	}
	f.assertPublicationEmpty(t)
}

func TestInvoiceMaterialRejectsStaleDuplicateOriginalRolesAndCrossTenant(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	first := f.upload(t, 10, "material-boundary-first")
	input := f.input(t, "add", "material-boundary-duplicate")
	input.DocumentID = first.DocumentID
	if _, err := f.service.Change(ctx, f.tenant, input, "duplicate"); !hasRuleCode(err, "invoice_material_exists") {
		t.Fatalf("duplicate: %v", err)
	}
	input.ExpectedVersion--
	if _, err := f.service.Change(ctx, f.tenant, input, "stale"); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale: %v", err)
	}
	var original string
	if err := f.store.DB().QueryRow(`SELECT c.document_id FROM invoices i JOIN review_decisions r ON r.id=i.source_review_decision_id JOIN claim_sets c ON c.id=r.claim_set_id WHERE i.id=?`, f.invoiceID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	input = f.input(t, "add", "material-original-reject")
	input.DocumentID = original
	if _, err := f.service.Change(ctx, f.tenant, input, "original"); !hasRuleCode(err, "invoice_material_is_original") {
		t.Fatalf("original: %v", err)
	}
	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleReviewer} {
		tenant := f.tenant
		tenant.Role = role
		if _, err := f.service.Workspace(ctx, tenant, f.invoiceID); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("unauthorized workspace")
		}
		if _, err := f.service.Change(ctx, tenant, input, "role"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("unauthorized mutation")
		}
	}
	other := domain.TenantContext{TenantID: mustID(t, system.IDGenerator{}), UserID: mustID(t, system.IDGenerator{}), Role: domain.RoleOwner}
	if _, err := f.service.Workspace(ctx, other, f.invoiceID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross tenant read")
	}
	if _, err := f.service.Change(ctx, other, input, "cross-tenant"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross tenant write")
	}
	bad := f.input(t, "upload", "material-stale-upload")
	bad.ExpectedVersion--
	if _, err := f.service.Upload(ctx, f.tenant, bad, documents.UploadInput{Name: "synthetic.png", MIME: "image/png", Source: bytes.NewReader(materialPNG(t, 11))}, "rollback"); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("rollback: %v", err)
	}
	f.assertPublicationEmpty(t)
	var count int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM documents WHERE original_name='synthetic.png'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed upload retained document")
	}
	for _, body := range [][]byte{[]byte("not an image"), nil} {
		if _, err := f.service.Upload(ctx, f.tenant, f.input(t, "upload", "material-invalid-image"), documents.UploadInput{Name: "bad.png", MIME: "image/png", Source: bytes.NewReader(body)}, "invalid"); err == nil {
			t.Fatal("invalid file accepted")
		}
	}
	f.assertPublicationEmpty(t)
}

func TestInvoiceMaterialSameSHAConcurrentInvoicesPreserveOneObject(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	s := NewService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	second := f
	second.invoiceID = confirmFactWithoutLinks(t, s, f.tenant, seedAdditionalReview(t, f.reviewFixture, invoiceEnvelope("SYN-RACE-002"), "material-race-second"), "material-race-confirm").FactID
	inputs := []domain.InvoiceMaterialRequest{f.input(t, "upload", "material-race-one"), second.input(t, "upload", "material-race-two")}
	results := make([]ports.InvoiceMaterialResult, 2)
	failures := make([]error, 2)
	var wg sync.WaitGroup
	for index := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], failures[i] = f.service.Upload(ctx, f.tenant, inputs[i], documents.UploadInput{Name: "race.png", MIME: "image/png", Source: bytes.NewReader(materialPNG(t, 40))}, fmt.Sprintf("race-%d", i))
		}(index)
	}
	wg.Wait()
	if failures[0] != nil || failures[1] != nil || results[0].DocumentID != results[1].DocumentID {
		t.Fatalf("concurrent SHA: %v", failures)
	}
	files := 0
	if err := filepath.WalkDir(filepath.Join(f.root, "objects"), func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files++
		}
		return nil
	}); err != nil || files != 1 {
		t.Fatal("shared object orphaned or missing")
	}
	f.assertPublicationEmpty(t)
}

func TestInvoiceMaterialPhysicalDeleteAndFactDeleteKeepHistory(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	added := f.upload(t, 51, "material-delete-upload")
	deletion := documents.NewDeletionService(f.store, f.objects, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	if err := deletion.Delete(ctx, f.tenant, added.DocumentID, "material-delete-blocked"); !hasRuleCode(err, "document_has_material_history") {
		t.Fatalf("physical delete: %v", err)
	}
	facts := NewFactService(f.store, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	if err := facts.Delete(ctx, f.tenant, domain.DocumentInvoice, f.invoiceID, "material-invoice-delete"); err != nil {
		t.Fatal(err)
	}
	var ended bool
	if err := f.store.DB().QueryRow(`SELECT ended_at IS NOT NULL AND ended_by_audit_event_id IS NOT NULL FROM invoice_material_links WHERE id=?`, added.LinkID).Scan(&ended); err != nil || !ended {
		t.Fatal("invoice deletion did not end materials")
	}
	if err := deletion.Delete(ctx, f.tenant, added.DocumentID, "material-history-delete-blocked"); !hasRuleCode(err, "document_has_material_history") {
		t.Fatalf("history delete: %v", err)
	}
	body, err := f.objects.Open(ctx, added.Document.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	content, err := io.ReadAll(body)
	if err != nil || !bytes.Equal(content, materialPNG(t, 51)) {
		t.Fatal("history original lost")
	}
	f.assertPublicationEmpty(t)
}

func TestInvoiceMaterialCreatedAfterDeletionPlanRestoresOriginal(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	upload := documents.NewUploadService(f.objects, f.inspector, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	document, err := upload.Execute(ctx, documents.UploadInput{Tenant: f.tenant, Name: "synthetic-delete-race.png", MIME: "image/png", Source: bytes.NewReader(materialPNG(t, 710))})
	if err != nil {
		t.Fatal(err)
	}
	repository := manualDuringDeletion{Store: f.store, start: func() { f.change(t, "add", document.DocumentID, "material-after-delete-plan") }}
	deletion := documents.NewDeletionService(repository, f.objects, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	if err := deletion.Delete(ctx, f.tenant, document.DocumentID, "material-stale-delete-plan"); !hasRuleCode(err, "document_has_material_history") {
		t.Fatalf("stale material delete: %v", err)
	}
	w, err := f.service.Workspace(ctx, f.tenant, f.invoiceID)
	if err != nil || len(w.Items) != 1 || w.Items[0].DocumentID != document.DocumentID {
		t.Fatal("new material relation lost")
	}
	query := documents.NewQueryService(f.store, f.store, f.objects)
	content, err := query.OpenDocument(ctx, f.tenant, document.DocumentID)
	if err != nil {
		t.Fatal("stale plan did not restore material original")
	}
	defer content.Body.Close()
	data, err := io.ReadAll(content.Body)
	if err != nil || !bytes.Equal(data, materialPNG(t, 710)) {
		t.Fatal("restored material differs")
	}
	f.assertPublicationEmpty(t)
}

type materialFailingTransaction struct {
	ports.TransactionManager
	failure error
	calls   int
}

func (f *materialFailingTransaction) WithinReadCommittedTransaction(ctx context.Context, fn func(ports.Transaction) error) error {
	f.calls++
	return f.TransactionManager.WithinReadCommittedTransaction(ctx, func(tx ports.Transaction) error {
		if err := fn(tx); err != nil {
			return err
		}
		if f.calls == 1 {
			return f.failure
		}
		return nil
	})
}

type materialFailingObjects struct {
	ports.ObjectStore
	commitFailure, deleteFailure error
}

func (s materialFailingObjects) Commit(ctx context.Context, staged ports.StagedObject, key string) error {
	if s.commitFailure != nil {
		return s.commitFailure
	}
	return s.ObjectStore.Commit(ctx, staged, key)
}
func (s materialFailingObjects) Delete(ctx context.Context, key string) error {
	if s.deleteFailure != nil {
		return s.deleteFailure
	}
	return s.ObjectStore.Delete(ctx, key)
}

func TestInvoiceMaterialPublicationAndFinalTransactionFailureAreRecoverable(t *testing.T) {
	for _, mode := range []string{"publish", "final-transaction", "compensation"} {
		t.Run(mode, func(t *testing.T) {
			f := newInvoiceMaterialFixture(t)
			ctx := context.Background()
			failure := errors.New("synthetic material failure")
			var tx ports.TransactionManager = f.store
			objects := materialFailingObjects{ObjectStore: f.objects}
			input := f.input(t, "upload", "material-injected-failure")
			if mode == "publish" {
				objects.commitFailure = failure
			}
			if mode == "final-transaction" {
				tx = &materialFailingTransaction{TransactionManager: f.store, failure: failure}
			}
			if mode == "compensation" {
				objects.deleteFailure = failure
				input.ExpectedVersion++
			}
			s := invoicematerials.NewService(f.store, tx, objects, f.objects, f.inspector, system.IDGenerator{}, fixedClock{now: f.now})
			if _, err := s.Upload(ctx, f.tenant, input, documents.UploadInput{Name: "synthetic-injected.png", MIME: "image/png", Source: bytes.NewReader(materialPNG(t, 720))}, "injected"); !errors.Is(err, failure) {
				t.Fatalf("failure hidden: %v", err)
			}
			var count int
			if err := f.store.DB().QueryRow(`SELECT (SELECT count(*) FROM invoice_material_links)+(SELECT count(*) FROM invoice_material_decisions)+(SELECT count(*) FROM documents WHERE original_name='synthetic-injected.png')`).Scan(&count); err != nil || count != 0 {
				t.Fatal("failed upload committed partial state")
			}
			if mode == "compensation" {
				pending, err := f.objects.PendingMaterialPublications(ctx, 100)
				if err != nil || len(pending) != 1 {
					t.Fatal("failed compensation lost intent")
				}
				if err := f.service.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
			}
			f.assertPublicationEmpty(t)
		})
	}
}

func TestInvoiceMaterialPublicationRecoveryRejectsChangedObject(t *testing.T) {
	f := newInvoiceMaterialFixture(t)
	ctx := context.Background()
	for _, state := range []string{"unpublished", "unused", "changed"} {
		staged, err := f.objects.Stage(ctx, strings.NewReader("synthetic original"), 100)
		if err != nil {
			t.Fatal(err)
		}
		doc := mustID(t, system.IDGenerator{})
		p := ports.MaterialPublication{ID: mustID(t, system.IDGenerator{}), TenantID: f.tenant.TenantID, DocumentID: doc, StorageKey: "tenants/" + f.tenant.TenantID + "/documents/" + doc + "/original", Staged: staged}
		if err := f.objects.RecordMaterialPublication(ctx, p); err != nil {
			t.Fatal(err)
		}
		if state != "unpublished" {
			if err := f.objects.Commit(ctx, staged, p.StorageKey); err != nil {
				t.Fatal(err)
			}
		}
		if state == "changed" {
			if err := os.WriteFile(filepath.Join(f.root, "objects", p.StorageKey), []byte("changed synthetic bytes"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		err = f.service.Reconcile(ctx)
		if state == "changed" {
			if !hasRuleCode(err, "invoice_material_object_changed") {
				t.Fatalf("changed recovery: %v", err)
			}
			if _, err := f.objects.GetMaterialPublication(ctx, p.ID); err != nil {
				t.Fatal("changed intent lost")
			}
			if err := os.WriteFile(filepath.Join(f.root, "objects", p.StorageKey), []byte("synthetic original"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if _, err := f.objects.Open(ctx, p.StorageKey); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("unused object retained")
		}
	}
	f.assertPublicationEmpty(t)
}
