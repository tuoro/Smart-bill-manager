package localstorage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestStoreStageInspectCommitAndOpen(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := store.Stage(context.Background(), bytes.NewReader(content), 1024)
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := NewInspector(store, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspector.InspectStaged(context.Background(), staged, "receipt.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.DetectedMIME != "image/png" || inspection.PageCount != 1 || inspection.Width != 1 || inspection.Height != 1 {
		t.Fatalf("inspection = %#v", inspection)
	}
	key := "tenants/t1/documents/d1/original"
	if err := store.Commit(context.Background(), staged, key); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("committed object differs from source")
	}
}

func TestStoreRejectsSizeAndTraversal(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stage(context.Background(), bytes.NewReader([]byte("12345")), 4); !errors.Is(err, domain.ErrPayloadTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}
	if _, err := store.Open(context.Background(), "../secret"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("traversal error = %v", err)
	}
}

func TestStoreLifecycleIsImmutableAndDeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	if _, err := New(" "); err == nil {
		t.Fatal("empty object root accepted")
	}
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stage(context.Background(), nil, 10); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("nil source error = %v", err)
	}
	if _, err := store.Stage(context.Background(), bytes.NewReader(nil), 10); err == nil {
		t.Fatal("empty object accepted")
	}
	if _, err := store.Stage(context.Background(), bytes.NewReader([]byte("content")), 0); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("zero limit error = %v", err)
	}

	key := "tenants/t1/documents/d1/original"
	first, err := store.Stage(context.Background(), bytes.NewReader([]byte("same")), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(context.Background(), first, key); err != nil {
		t.Fatal(err)
	}
	second, err := store.Stage(context.Background(), bytes.NewReader([]byte("same")), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(context.Background(), second, key); err != nil {
		t.Fatalf("idempotent object commit failed: %v", err)
	}
	if err := store.Abort(context.Background(), second); err != nil {
		t.Fatalf("abort after idempotent commit failed: %v", err)
	}
	different, err := store.Stage(context.Background(), bytes.NewReader([]byte("other")), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(context.Background(), different, key); err == nil {
		t.Fatal("immutable object was overwritten")
	}
	if err := store.Abort(context.Background(), different); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatalf("idempotent delete failed: %v", err)
	}
	if _, err := store.Open(context.Background(), key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted object open error = %v", err)
	}
	if err := store.Delete(context.Background(), "../escape"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("unsafe delete error = %v", err)
	}
}

func TestRecoverableDeletionCanRestoreOrPurgeWholeBatch(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{
		"tenants/t1/documents/d1/original",
		"tenants/t1/documents/d1/pages/0001.png",
	}
	for index, key := range keys {
		staged, err := store.Stage(context.Background(), bytes.NewReader([]byte{byte(index + 1)}), 10)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Commit(context.Background(), staged, key); err != nil {
			t.Fatal(err)
		}
	}
	firstDeletion := "00000000-0000-4000-8000-000000000101"
	if err := store.StageDeletion(context.Background(), firstDeletion, keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if _, err := store.Open(context.Background(), key); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("staged deletion remained readable: %s / %v", key, err)
		}
	}
	pending, err := store.PendingDeletions(context.Background())
	if err != nil || len(pending) != 1 || pending[0] != firstDeletion {
		t.Fatalf("pending deletions = %#v, error=%v", pending, err)
	}
	if err := store.RestoreDeletion(context.Background(), firstDeletion); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		reader, err := store.Open(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		_ = reader.Close()
	}
	pending, err = store.PendingDeletions(context.Background())
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after restore = %#v, error=%v", pending, err)
	}

	secondDeletion := "00000000-0000-4000-8000-000000000102"
	if err := store.StageDeletion(context.Background(), secondDeletion, keys); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeDeletion(context.Background(), secondDeletion); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeDeletion(context.Background(), secondDeletion); err != nil {
		t.Fatalf("idempotent purge failed: %v", err)
	}
	for _, key := range keys {
		if _, err := store.Open(context.Background(), key); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("purged object remained readable: %s / %v", key, err)
		}
	}
}

func TestRecoverableDeletionRollsBackPartialStageFailure(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "tenants/t1/documents/d1/original"
	staged, err := store.Stage(context.Background(), bytes.NewReader([]byte("content")), 20)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(context.Background(), staged, key); err != nil {
		t.Fatal(err)
	}
	deletionID := "00000000-0000-4000-8000-000000000103"
	if err := store.StageDeletion(context.Background(), deletionID, []string{key, "tenants/t1/documents/d1/missing"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("partial deletion error = %v", err)
	}
	reader, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("partial deletion did not restore first object: %v", err)
	}
	_ = reader.Close()
	pending, err := store.PendingDeletions(context.Background())
	if err != nil || len(pending) != 0 {
		t.Fatalf("failed stage left batch = %#v, error=%v", pending, err)
	}
	if err := store.StageDeletion(context.Background(), "invalid", []string{key}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("invalid deletion id error = %v", err)
	}
}

func TestStoreCancellationAndCopyFailureBoundaries(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Stage(ctx, bytes.NewReader([]byte("content")), 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stage error = %v", err)
	}
	if err := store.Abort(context.Background(), ports.StagedObject{ID: "invalid"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("unsafe abort error = %v", err)
	}
	if safeUUID("00000000-0000-4000-8000-000000000001") != true || safeUUID("00000000-0000-4000-8000-00000000000g") != false {
		t.Fatal("UUID safety classification is incorrect")
	}
	if _, err := copyContext(context.Background(), shortWriter{}, bytes.NewReader([]byte("value"))); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v", err)
	}
	if _, err := copyContext(context.Background(), io.Discard, failingReader{}); err == nil {
		t.Fatal("reader failure ignored")
	}
}

type shortWriter struct{}

func (shortWriter) Write(value []byte) (int, error) { return len(value) - 1, nil }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestInspectorRejectsSignatureMismatch(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content, _ := base64.StdEncoding.DecodeString(onePixelPNG)
	staged, err := store.Stage(context.Background(), bytes.NewReader(content), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Abort(context.Background(), staged)
	inspector, _ := NewInspector(store, "/bin/false")
	_, err = inspector.InspectStaged(context.Background(), staged, "receipt.jpg", "image/jpeg")
	var ruleError *domain.RuleError
	if !errors.As(err, &ruleError) || ruleError.Code != "document_signature_mismatch" {
		t.Fatalf("signature error = %v", err)
	}
	_, err = inspector.InspectStaged(context.Background(), staged, "receipt.txt", "text/plain")
	if !errors.As(err, &ruleError) || ruleError.Code != "unsupported_document" {
		t.Fatalf("unsupported document error = %v", err)
	}
}

func TestSupportedSignaturesAndExtensionsAreExact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header []byte
		mime   string
		valid  []string
	}{
		{name: "jpeg", header: []byte{0xff, 0xd8, 0xff}, mime: "image/jpeg", valid: []string{"receipt.jpg", "receipt.JPEG"}},
		{name: "png", header: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, mime: "image/png", valid: []string{"receipt.png"}},
		{name: "webp", header: []byte("RIFF1234WEBP"), mime: "image/webp", valid: []string{"receipt.webp"}},
		{name: "pdf", header: []byte("%PDF-1.7"), mime: "application/pdf", valid: []string{"invoice.PDF"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := detectMIME(test.header); got != test.mime {
				t.Fatalf("detected MIME = %q, want %q", got, test.mime)
			}
			for _, name := range test.valid {
				if !extensionMatches(name, test.mime) {
					t.Fatalf("valid extension rejected: %s / %s", name, test.mime)
				}
			}
			if extensionMatches("receipt.txt", test.mime) {
				t.Fatalf("invalid extension accepted for %s", test.mime)
			}
		})
	}
	if got := detectMIME([]byte("not-a-document")); got != "" {
		t.Fatalf("unsupported signature = %q", got)
	}
	if extensionMatches("receipt.bin", "application/octet-stream") {
		t.Fatal("unsupported MIME extension accepted")
	}
	if supportedDocumentMIME("text/plain") || !supportedDocumentMIME("application/pdf") {
		t.Fatal("supported MIME classification is incorrect")
	}
}

func TestInspectorAcceptsLargeImageForIsolatedNormalization(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 8001, 1))
	var content bytes.Buffer
	if err := jpeg.Encode(&content, canvas, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	staged, err := store.Stage(context.Background(), bytes.NewReader(content.Bytes()), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Abort(context.Background(), staged)
	inspector, _ := NewInspector(store, "/bin/false")
	inspection, err := inspector.InspectStaged(context.Background(), staged, "wide.jpg", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Width != 8001 || inspection.Height != 1 {
		t.Fatalf("large image inspection = %#v", inspection)
	}
}

func TestNormalizerProducesEightBitProviderPNGForJPEG(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 32, 24))
	var source bytes.Buffer
	if err := jpeg.Encode(&source, canvas, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	staged, err := store.Stage(context.Background(), bytes.NewReader(source.Bytes()), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	storageKey := "tenants/t1/documents/d1/original"
	if err := store.Commit(context.Background(), staged, storageKey); err != nil {
		t.Fatal(err)
	}
	normalizer, err := NewNormalizer(store, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := normalizer.Normalize(context.Background(), ports.ProcessingDocument{
		TenantID: "t1", DocumentID: "d1", StorageKey: storageKey, MIME: "image/jpeg", PageCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || len(pages[0].Data) < 26 {
		t.Fatalf("normalized pages = %#v", pages)
	}
	if bitDepth := pages[0].Data[24]; bitDepth != 8 {
		t.Fatalf("normalized PNG bit depth = %d, want 8", bitDepth)
	}
}

func TestPDFInspectionBoundaries(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staged, err := store.Stage(context.Background(), bytes.NewReader([]byte("%PDF-1.7\nsynthetic")), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Abort(context.Background(), staged)
	command := filepath.Join(t.TempDir(), "pdfinfo")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf 'Pages: 2\\nEncrypted: no\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	inspector, _ := NewInspector(store, command)
	inspection, err := inspector.InspectStaged(context.Background(), staged, "invoice.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.PageCount != 2 {
		t.Fatalf("page count = %d", inspection.PageCount)
	}
}

func TestPDFInspectionRejectsEncryptedOversizedAndCorruptDocuments(t *testing.T) {
	t.Parallel()

	for name, script := range map[string]string{
		"encrypted": "#!/bin/sh\nprintf 'Pages: 2\\nEncrypted: yes\\n'\n",
		"too-many":  "#!/bin/sh\nprintf 'Pages: 21\\nEncrypted: no\\n'\n",
		"no-pages":  "#!/bin/sh\nprintf 'Encrypted: no\\n'\n",
		"failure":   "#!/bin/sh\nexit 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			store, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			staged, err := store.Stage(context.Background(), bytes.NewReader([]byte("%PDF-1.7\nsynthetic")), 1024)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Abort(context.Background(), staged)
			command := filepath.Join(t.TempDir(), "pdfinfo")
			if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			inspector, _ := NewInspector(store, command)
			if _, err := inspector.InspectStaged(context.Background(), staged, "invoice.pdf", "application/pdf"); err == nil {
				t.Fatalf("invalid PDF %s accepted", name)
			}
		})
	}
}

func TestNormalizerDeleteAndResizeBoundaries(t *testing.T) {
	t.Parallel()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := NewNormalizer(store, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := store.Stage(context.Background(), bytes.NewReader([]byte("derived")), 20)
	if err != nil {
		t.Fatal(err)
	}
	key := "tenants/t1/documents/d1/pages/0001.png"
	if err := store.Commit(context.Background(), staged, key); err != nil {
		t.Fatal(err)
	}
	if err := normalizer.DeleteNormalized(context.Background(), []ports.NormalizedPage{{StorageKey: key}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted normalized page error = %v", err)
	}

	horizontal := image.NewRGBA(image.Rect(0, 0, 10, 2))
	if bounds := resizeImage(horizontal, 5).Bounds(); bounds.Dx() != 5 || bounds.Dy() != 1 {
		t.Fatalf("horizontal resize = %s", bounds)
	}
	vertical := image.NewRGBA(image.Rect(0, 0, 2, 10))
	if bounds := resizeImage(vertical, 5).Bounds(); bounds.Dx() != 1 || bounds.Dy() != 5 {
		t.Fatalf("vertical resize = %s", bounds)
	}
	unchanged := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if resizeImage(unchanged, 5) != unchanged {
		t.Fatal("small image was unnecessarily copied")
	}
}
