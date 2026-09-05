package materialexports

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type exportRepository func(context.Context, string, domain.ExportScope) (ports.ExportInventory, error)

func (f exportRepository) BuildMaterialExport(ctx context.Context, tenant string, scope domain.ExportScope) (ports.ExportInventory, error) {
	return f(ctx, tenant, scope)
}

type failedIDs struct{}

func (failedIDs) NewID() (string, error) { return "", errors.New("synthetic ID error") }

type failedSpool struct{}

func (failedSpool) CreateExportFile(context.Context) (ports.ExportFile, error) {
	return nil, errors.New("synthetic disk full")
}

func exportFixture(t *testing.T) (*Service, Actor, ports.ExportInventory) {
	t.Helper()
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.InitializeExportSpool(); err != nil {
		t.Fatal(err)
	}
	data := []byte("synthetic original bytes")
	staged, err := objects.Stage(context.Background(), bytes.NewReader(data), 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Commit(context.Background(), staged, "synthetic/original"); err != nil {
		t.Fatal(err)
	}
	inventory := ports.ExportInventory{Manifest: domain.ExportManifest{Scope: domain.ExportScope{Kind: "trip", ID: "trip-synthetic"}, Name: "合成行程", Version: 1, MaterialsCaptured: true,
		References: []domain.ExportReference{{Kind: "original", RelationID: "assignment-a", FactID: "payment-a", FactType: "payment", DocumentID: "document-a"}, {Kind: "auxiliary", RelationID: "material-a", FactID: "invoice-a", FactType: "invoice", DocumentID: "document-a"}},
		Files:      []domain.ExportFile{{DocumentID: "document-a", OriginalName: "../synthetic.png", MIME: "image/png", SizeBytes: staged.Size, SHA256: staged.SHA256}}}, StorageKeys: map[string]string{"document-a": "synthetic/original"}}
	service := NewService(exportRepository(func(context.Context, string, domain.ExportScope) (ports.ExportInventory, error) {
		return inventory, nil
	}), objects, objects, system.IDGenerator{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	return service, Actor{Tenant: domain.TenantContext{TenantID: "tenant-a", UserID: "user-a", Role: domain.RoleOwner}, SessionID: "session-a"}, inventory
}

func prepareFixture(t *testing.T, s *Service, actor Actor, scope domain.ExportScope) Prepared {
	t.Helper()
	manifest, err := s.Preview(context.Background(), actor.Tenant, scope)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Prepare(context.Background(), actor, scope, manifest.ManifestHash, true)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMaterialExportCompleteZIPAndOneTimeSessionBoundDownload(t *testing.T) {
	s, actor, inventory := exportFixture(t)
	prepared := prepareFixture(t, s, actor, inventory.Manifest.Scope)
	for _, field := range []string{"tenant", "user", "session"} {
		other := actor
		switch field {
		case "tenant":
			other.Tenant.TenantID = "other"
		case "user":
			other.Tenant.UserID = "other"
		case "session":
			other.SessionID = "other"
		}
		if _, err := s.Take(other, prepared.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("cross-identity download accepted")
		}
	}
	download, err := s.Take(actor, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Take(actor, prepared.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("package consumed twice")
	}
	body, err := io.ReadAll(download.Body)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(body)) != prepared.SizeBytes {
		t.Fatal("archive content length differs")
	}
	if err := download.Body.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 3 {
		t.Fatal("shared object duplicated or manifest missing")
	}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Method != zip.Store {
			t.Fatal("archive unexpectedly recompresses objects")
		}
		if file.Name == "manifest.json" {
			if bytes.Contains(contents, []byte("storage_key")) || bytes.Contains(contents, []byte("synthetic/original")) {
				t.Fatal("internal storage identity leaked")
			}
			var m domain.ExportManifest
			if err := json.Unmarshal(contents, &m); err != nil || m.ManifestHash != prepared.ManifestHash || len(m.References) != 2 {
				t.Fatal("manifest lost references")
			}
		}
	}
	if len(s.slots) != 0 || len(s.active) != 0 || len(s.ready) != 0 {
		t.Fatal("download leaked resources")
	}
}

func TestMaterialExportBoundsCancelExpiryAndShutdown(t *testing.T) {
	s, actor, inventory := exportFixture(t)
	first := prepareFixture(t, s, actor, inventory.Manifest.Scope)
	second := prepareFixture(t, s, actor, inventory.Manifest.Scope)
	if _, err := s.Prepare(context.Background(), actor, inventory.Manifest.Scope, first.ManifestHash, true); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("third slot accepted")
	}
	if err := s.Cancel(actor, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(actor, first.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cancel returned false success")
	}
	download, err := s.Take(actor, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.readyLifetime = 10 * time.Millisecond
	third := prepareFixture(t, s, actor, inventory.Manifest.Scope)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		_, exists := s.ready[third.ID]
		s.mu.Unlock()
		if !exists {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := s.Take(actor, third.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("expired package readable")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := download.Body.Read(make([]byte, 1)); err == nil {
		t.Fatal("shutdown retained download")
	}
	if len(s.slots) != 0 {
		t.Fatal("shutdown leaked slots")
	}
	if _, err := s.Prepare(context.Background(), actor, inventory.Manifest.Scope, first.ManifestHash, true); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("closed service prepared package")
	}
}

func TestMaterialExportPreviewChangedMissingDamagedAndRejectedInputs(t *testing.T) {
	for _, kind := range []string{"scope", "repository", "stale", "ack", "missing", "short", "long", "hash", "key", "disk", "id", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			s, actor, inventory := exportFixture(t)
			manifest, err := s.Preview(context.Background(), actor.Tenant, inventory.Manifest.Scope)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			ack := true
			switch kind {
			case "scope":
				inventory.Manifest.Scope.ID = "wrong"
			case "repository":
				s.repository = exportRepository(func(context.Context, string, domain.ExportScope) (ports.ExportInventory, error) {
					return ports.ExportInventory{}, errors.New("synthetic database failure")
				})
			case "stale":
				inventory.Manifest.Version++
			case "ack":
				inventory.Manifest.Warnings = []string{"历史限制"}
				ack = false
			case "missing":
				if err := s.objects.Delete(ctx, "synthetic/original"); err != nil {
					t.Fatal(err)
				}
			case "short":
				inventory.Manifest.Files[0].SizeBytes++
			case "long":
				inventory.Manifest.Files[0].SizeBytes--
			case "hash":
				inventory.Manifest.Files[0].SHA256 = strings.Repeat("f", 64)
			case "key":
				inventory.StorageKeys = map[string]string{}
			case "disk":
				s.spool = failedSpool{}
			case "id":
				s.ids = failedIDs{}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if kind != "repository" {
				s.repository = exportRepository(func(context.Context, string, domain.ExportScope) (ports.ExportInventory, error) {
					return inventory, nil
				})
			}
			if kind == "ack" || kind == "short" || kind == "long" || kind == "hash" {
				manifest, err = s.Preview(context.Background(), actor.Tenant, inventory.Manifest.Scope)
				if err != nil {
					t.Fatal(err)
				}
			}
			if result, err := s.Prepare(ctx, actor, domain.ExportScope{Kind: "trip", ID: "trip-synthetic"}, manifest.ManifestHash, ack); err == nil || result.ID != "" {
				t.Fatal("failed export produced success")
			}
			if len(s.slots) != 0 || len(s.ready) != 0 || len(s.active) != 0 {
				t.Fatal("failed export leaked resources")
			}
		})
	}
	s, actor, inventory := exportFixture(t)
	for _, role := range []domain.Role{domain.RoleReviewer, domain.RoleViewer} {
		other := actor
		other.Tenant.Role = role
		if _, err := s.Preview(context.Background(), other.Tenant, inventory.Manifest.Scope); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("role bypass")
		}
		if _, err := s.Take(other, "unknown"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal("download role bypass")
		}
	}
	if _, err := s.Take(actor, "../id"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("path accepted")
	}
	actor.SessionID = ""
	if _, err := s.Prepare(context.Background(), actor, inventory.Manifest.Scope, strings.Repeat("a", 64), true); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("sessionless prepare")
	}
}

func TestMaterialExportConcurrentTakeHasExactlyOneReader(t *testing.T) {
	s, actor, inventory := exportFixture(t)
	prepared := prepareFixture(t, s, actor, inventory.Manifest.Scope)
	var wg sync.WaitGroup
	results := make(chan Download, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := s.Take(actor, prepared.ID)
			if err == nil {
				results <- d
			} else if !errors.Is(err, domain.ErrNotFound) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(results)
	count := 0
	for d := range results {
		count++
		d.Body.Close()
	}
	if count != 1 || len(s.slots) != 0 {
		t.Fatal("concurrent readers or resource leak")
	}
}

func TestMaterialExportSpoolWriteLimitAndStorageErrors(t *testing.T) {
	writer := &limitedExportWriter{writer: io.Discard, remaining: 3}
	if _, err := writer.Write([]byte("1234")); !errors.Is(err, domain.ErrPayloadTooLarge) || writer.remaining != 3 {
		t.Fatal("ZIP output cap ignored")
	}
	if _, err := writer.Write([]byte("123")); err != nil || writer.remaining != 0 {
		t.Fatal("exact boundary failed")
	}
	if !errors.Is(archiveWriteError(errors.New("synthetic filesystem error")), domain.ErrUnavailable) {
		t.Fatal("disk error not exposed")
	}
}

type repeatedByteReader struct {
	value     byte
	remaining int64
}

func (r *repeatedByteReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(buffer)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := 0; i < n; i++ {
		buffer[i] = r.value
	}
	r.remaining -= int64(n)
	return n, nil
}

type generatedExportObjects struct {
	ports.ObjectStore
	sizes  map[string]int64
	values map[string]byte
}

func (s generatedExportObjects) Open(_ context.Context, key string) (io.ReadCloser, error) {
	size, ok := s.sizes[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(&repeatedByteReader{value: s.values[key], remaining: size}), nil
}

func TestMaterialExport64MiBUsesBoundedHeapAndDiskSpool(t *testing.T) {
	s, actor, inventory := exportFixture(t)
	inventory.Manifest.Files = nil
	inventory.Manifest.References = nil
	inventory.StorageKeys = map[string]string{}
	objects := generatedExportObjects{sizes: map[string]int64{}, values: map[string]byte{}}
	const fileSize = int64(16 * 1024 * 1024)
	buffer := make([]byte, 32*1024)
	for index := 0; index < 4; index++ {
		id := fmt.Sprintf("stream-document-%d", index)
		key := "synthetic/" + id
		value := byte(index + 1)
		hash := sha256.New()
		if _, err := io.CopyBuffer(hash, &repeatedByteReader{value: value, remaining: fileSize}, buffer); err != nil {
			t.Fatal(err)
		}
		inventory.Manifest.Files = append(inventory.Manifest.Files, domain.ExportFile{DocumentID: id, OriginalName: id + ".pdf", MIME: "application/pdf", SizeBytes: fileSize, SHA256: hex.EncodeToString(hash.Sum(nil))})
		inventory.Manifest.References = append(inventory.Manifest.References, domain.ExportReference{Kind: "original", RelationID: id, FactID: id, DocumentID: id})
		inventory.StorageKeys[id] = key
		objects.sizes[key] = fileSize
		objects.values[key] = value
	}
	s.objects = objects
	s.repository = exportRepository(func(context.Context, string, domain.ExportScope) (ports.ExportInventory, error) {
		return inventory, nil
	})
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	prepared := prepareFixture(t, s, actor, inventory.Manifest.Scope)
	if prepared.SizeBytes <= 4*fileSize || prepared.SizeBytes > 4*fileSize+1024*1024 {
		t.Fatal("archive size unexpectedly differs from Store input")
	}
	download, err := s.Take(actor, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	written, err := io.CopyBuffer(io.Discard, download.Body, buffer)
	if err != nil || written != prepared.SizeBytes {
		t.Fatal("streaming download incomplete")
	}
	if err := download.Body.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if after.TotalAlloc-before.TotalAlloc > 8*1024*1024 {
		t.Fatal("64 MiB package allocated over 8 MiB heap")
	}
	if len(s.active) != 0 || len(s.ready) != 0 || len(s.slots) != 0 {
		t.Fatal("streaming package leaked resources")
	}
}

func TestMaterialExportPreparingSlotsAndCancellationAreBounded(t *testing.T) {
	s, actor, inventory := exportFixture(t)
	manifest, err := s.Preview(context.Background(), actor.Tenant, inventory.Manifest.Scope)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	s.repository = exportRepository(func(ctx context.Context, _ string, _ domain.ExportScope) (ports.ExportInventory, error) {
		started <- struct{}{}
		<-ctx.Done()
		return ports.ExportInventory{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := s.Prepare(ctx, actor, inventory.Manifest.Scope, manifest.ManifestHash, true)
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("prepare did not start")
		}
	}
	if _, err := s.Prepare(context.Background(), actor, inventory.Manifest.Scope, manifest.ManifestHash, true); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("preparing did not reserve slot")
	}
	cancel()
	for range 2 {
		select {
		case err := <-results:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation hidden")
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled prepare stuck")
		}
	}
	if len(s.slots) != 0 {
		t.Fatal("cancelled preparing slots retained")
	}
}
