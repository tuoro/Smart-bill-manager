package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/processing"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/providers"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type httpTestFixture struct {
	store         *sqliteadapter.Store
	handler       http.Handler
	worker        *processing.Worker
	owner         bootstrap.Result
	ownerEmail    string
	ownerPassword string
}

type testSession struct {
	cookies []*http.Cookie
	csrf    string
}

type passingDetector struct{}

func (passingDetector) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/1", SHA256: strings.Repeat("c", 64)}
}

func (passingDetector) DetectCapabilities(context.Context, ports.ProviderCredentials) ports.CapabilityResult {
	return ports.CapabilityResult{Passed: true, SafeMessage: "能力检测通过"}
}

type staticExtractor struct{}

func (staticExtractor) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/1", SHA256: strings.Repeat("c", 64)}
}

func (staticExtractor) Prepare(_ ports.ProviderCredentials, pages []ports.PageImage) (ports.PreparedBillExtraction, error) {
	if len(pages) != 1 || pages[0].PageNumber != 1 || len(pages[0].Data) == 0 {
		return nil, errors.New("expected one normalized page")
	}
	return staticPreparedExtraction{}, nil
}

type staticPreparedExtraction struct{}

func (staticPreparedExtraction) RequestHash() string { return strings.Repeat("a", 64) }

func (staticPreparedExtraction) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/1", SHA256: strings.Repeat("c", 64)}
}

func (staticPreparedExtraction) Execute(context.Context) (ports.BillExtractionResult, error) {
	return ports.BillExtractionResult{
		Envelope: domain.BillVisibleTextEnvelope{
			SchemaVersion: "bill-visible-text/1",
			DocumentType:  string(domain.DocumentPayment),
			Payment:       json.RawMessage(`{"amount":{"text":"CNY 12.34","page":1},"currency":{"text":"CNY","page":1},"merchant":{"text":"合成商户","page":1},"transaction_time":{"text":"2026-08-27 10:30","page":1},"timezone":null,"payment_method":null,"order_number":null,"category":null}`),
			Invoice:       json.RawMessage(`null`),
		},
		ResponseHash: strings.Repeat("b", 64),
		InputTokens:  20,
		OutputTokens: 30,
		Latency:      25 * time.Millisecond,
	}, nil
}

type readyFixture struct{}

func (readyFixture) Ready(context.Context) error { return nil }

type testPasswordHasher struct{}

func (testPasswordHasher) Hash(password []byte) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", errors.New("invalid password")
	}
	return "test-password-hash:v1:" + hex.EncodeToString(password), nil
}

func (testPasswordHasher) Verify(password []byte, encoded string) (bool, error) {
	expected, err := (testPasswordHasher{}).Hash(password)
	if err != nil {
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(encoded)) == 1, nil
}

func TestHTTPWorkflowAndTenantIsolation(t *testing.T) {
	fixture := newHTTPTestFixture(t)
	defer fixture.store.Close()

	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/payments", nil, nil, false, ""), http.StatusUnauthorized)
	wrongLogin := "{\"email\":\"owner@example.test\",\"password\":\"wrong-password\"}"
	assertStatus(t, fixture.request(http.MethodPost, "/api/v1/session/login", strings.NewReader(wrongLogin), nil, false, "application/json"), http.StatusUnauthorized)

	ownerSession := fixture.login(t, fixture.owner.TenantID)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/session", nil, ownerSession, false, ""), http.StatusOK)
	providerJSON := "{\"base_url\":\"https://provider.example/v1\",\"api_key\":\"synthetic-key\",\"model\":\"vision-model\",\"output_mode\":\"json_schema\"}"
	assertStatus(t, fixture.request(http.MethodPost, "/api/v1/provider-configs", strings.NewReader(providerJSON), ownerSession, false, "application/json"), http.StatusForbidden)

	createProvider := fixture.request(http.MethodPost, "/api/v1/provider-configs", strings.NewReader(providerJSON), ownerSession, true, "application/json")
	assertStatus(t, createProvider, http.StatusCreated)
	provider := decodeMap(t, createProvider)
	providerID := asString(t, provider["id"])
	if provider["capability_status"] != "pending" || provider["output_mode"] != "json_schema" || bytes.Contains(createProvider.Body.Bytes(), []byte("synthetic-key")) {
		t.Fatalf("unsafe provider response: %s", createProvider.Body.String())
	}
	assertStatus(t, fixture.request(http.MethodPost, "/api/v1/provider-configs/"+providerID+"/activate", nil, ownerSession, true, ""), http.StatusConflict)
	detection := fixture.request(http.MethodPost, "/api/v1/provider-configs/"+providerID+"/detect", nil, ownerSession, true, "")
	assertStatus(t, detection, http.StatusOK)
	detectedProvider := decodeMap(t, detection)
	if detectedProvider["capability_schema_version"] != "bill-visible-text-provider/1" ||
		detectedProvider["capability_schema_sha256"] != strings.Repeat("c", 64) {
		t.Fatalf("provider detection schema identity = %#v", detectedProvider)
	}
	assertStatus(t, fixture.request(http.MethodPost, "/api/v1/provider-configs/"+providerID+"/activate", nil, ownerSession, true, ""), http.StatusOK)
	providerList := fixture.request(http.MethodGet, "/api/v1/provider-configs", nil, ownerSession, false, "")
	assertStatus(t, providerList, http.StatusOK)
	if bytes.Contains(providerList.Body.Bytes(), []byte("synthetic-key")) {
		t.Fatal("provider list exposed API key")
	}

	firstDocument := syntheticPNG(t, color.RGBA{R: 40, G: 80, B: 180, A: 255})
	firstUpload := fixture.upload(t, ownerSession, "payment.png", "image/png", firstDocument)
	assertStatus(t, fixture.uploadRaw(t, ownerSession, "payment.png", "image/png", firstDocument), http.StatusConflict)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/jobs?status=unsupported", nil, ownerSession, false, ""), http.StatusBadRequest)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/jobs/"+firstUpload["job_id"], nil, ownerSession, false, ""), http.StatusOK)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"], nil, ownerSession, false, ""), http.StatusOK)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"]+"/content", nil, ownerSession, false, ""), http.StatusOK)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"]+"/pages/1/content", nil, ownerSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"]+"/pages/not-a-page/content", nil, ownerSession, false, ""), http.StatusBadRequest)

	fixture.processNext(t)
	pageContent := fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"]+"/pages/1/content", nil, ownerSession, false, "")
	assertStatus(t, pageContent, http.StatusOK)
	if pageContent.Header().Get("Content-Type") != "image/png" || pageContent.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("unsafe document page headers: %#v", pageContent.Header())
	}
	var runPromptVersion, runExtractionSchemaVersion, runProviderSchemaVersion, runProviderSchemaSHA256, runClaimMapperVersion string
	if err := fixture.store.DB().QueryRow(`
		SELECT prompt_version, extraction_schema_version, provider_schema_version, provider_schema_sha256, claim_mapper_version
		FROM ai_runs WHERE tenant_id = ? ORDER BY started_at DESC LIMIT 1
	`, fixture.owner.TenantID).Scan(&runPromptVersion, &runExtractionSchemaVersion, &runProviderSchemaVersion, &runProviderSchemaSHA256, &runClaimMapperVersion); err != nil {
		t.Fatal(err)
	}
	if runPromptVersion != "bill-visible-text-cn/1" || runExtractionSchemaVersion != "bill-visible-text/1" ||
		runProviderSchemaVersion != "bill-visible-text-provider/1" || runProviderSchemaSHA256 != strings.Repeat("c", 64) ||
		runClaimMapperVersion != "claim-mapper/3" {
		t.Fatalf("frozen AI run schema identity = %s/%s/%s/%s/%s", runPromptVersion, runExtractionSchemaVersion, runProviderSchemaVersion, runProviderSchemaSHA256, runClaimMapperVersion)
	}
	reviewResponse := fixture.request(http.MethodGet, "/api/v1/reviews/"+firstUpload["job_id"], nil, ownerSession, false, "")
	assertStatus(t, reviewResponse, http.StatusOK)
	review := decodeMap(t, reviewResponse)
	if asInt(t, review["revision"]) != 1 || review["claim_status"] != "ready_for_review" {
		t.Fatalf("unexpected review: %+v", review)
	}
	if _, ok := review["duplicate_candidates"].([]any); !ok {
		t.Fatalf("review duplicate_candidates is not an array: %+v", review["duplicate_candidates"])
	}
	if asInt(t, review["page_count"]) != 1 || len(review["pages"].([]any)) != 1 || len(review["invoice_item_spans"].([]any)) != 0 {
		t.Fatalf("review page plan is incomplete: %+v", review)
	}
	claimSetID := asString(t, review["claim_set_id"])
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/claim-sets/"+claimSetID, nil, ownerSession, false, ""), http.StatusOK)
	revisionResponse := fixture.request(http.MethodPost, "/api/v1/reviews/"+firstUpload["job_id"]+"/revisions", bytes.NewReader(revisionPayload(t, review)), ownerSession, true, "application/json")
	assertStatus(t, revisionResponse, http.StatusCreated)
	review = decodeMap(t, revisionResponse)
	if asInt(t, review["revision"]) != 2 {
		t.Fatalf("revision = %v", review["revision"])
	}
	missingAllocations := fixture.requestWithHeaders(
		http.MethodPost,
		"/api/v1/reviews/"+firstUpload["job_id"]+"/confirm",
		strings.NewReader("{\"expected_revision\":2,\"association_mode\":\"no_candidate\"}"),
		ownerSession,
		true,
		"application/json",
		map[string]string{"Idempotency-Key": "missing-allocations"},
	)
	assertStatus(t, missingAllocations, http.StatusBadRequest)
	missingDuplicateResolutions := fixture.requestWithHeaders(
		http.MethodPost,
		"/api/v1/reviews/"+firstUpload["job_id"]+"/confirm",
		strings.NewReader("{\"expected_revision\":2,\"association_mode\":\"no_candidate\",\"allocations\":[]}"),
		ownerSession,
		true,
		"application/json",
		map[string]string{"Idempotency-Key": "missing-duplicate-resolutions"},
	)
	assertStatus(t, missingDuplicateResolutions, http.StatusBadRequest)

	invalidConfirm := fixture.requestWithHeaders(
		http.MethodPost,
		"/api/v1/reviews/"+firstUpload["job_id"]+"/confirm",
		strings.NewReader("{\"expected_revision\":2,\"association_mode\":\"reject_all\",\"allocations\":[],\"duplicate_resolutions\":[]}"),
		ownerSession,
		true,
		"application/json",
		map[string]string{"Idempotency-Key": "invalid-confirm"},
	)
	assertStatus(t, invalidConfirm, http.StatusConflict)

	confirmJSON := "{\"expected_revision\":2,\"association_mode\":\"no_candidate\",\"allocations\":[],\"duplicate_resolutions\":[]}"
	confirm := fixture.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+firstUpload["job_id"]+"/confirm", strings.NewReader(confirmJSON), ownerSession, true, "application/json", map[string]string{"Idempotency-Key": "confirm-0001"})
	assertStatus(t, confirm, http.StatusOK)
	confirmed := decodeMap(t, confirm)
	if confirmed["fact_type"] != "payment" || confirmed["fact_id"] == "" || len(confirmed["link_ids"].([]any)) != 0 {
		t.Fatalf("unexpected confirm response: %+v", confirmed)
	}
	replay := fixture.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+firstUpload["job_id"]+"/confirm", strings.NewReader(confirmJSON), ownerSession, true, "application/json", map[string]string{"Idempotency-Key": "confirm-0001"})
	assertStatus(t, replay, http.StatusOK)
	replayed := decodeMap(t, replay)
	if replayed["fact_id"] != confirmed["fact_id"] {
		t.Fatalf("replay changed fact: first=%v replay=%v", confirmed, replayed)
	}
	confirmedClaim := fixture.request(http.MethodGet, "/api/v1/claim-sets/"+asString(t, review["claim_set_id"]), nil, ownerSession, false, "")
	assertStatus(t, confirmedClaim, http.StatusOK)
	if decodeMap(t, confirmedClaim)["claim_status"] != "confirmed" {
		t.Fatalf("confirmed ClaimSet detail = %s", confirmedClaim.Body.String())
	}
	payments := fixture.request(http.MethodGet, "/api/v1/payments", nil, ownerSession, false, "")
	assertStatus(t, payments, http.StatusOK)
	if !strings.Contains(payments.Body.String(), "\"amount_minor\":1234") ||
		!strings.Contains(payments.Body.String(), "\"allocated_minor\":0") ||
		!strings.Contains(payments.Body.String(), "\"remaining_minor\":1234") ||
		!strings.Contains(payments.Body.String(), "\"allocation_status\":\"unallocated\"") {
		t.Fatalf("payment missing: %s", payments.Body.String())
	}
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/invoices", nil, ownerSession, false, ""), http.StatusOK)

	secondUpload := fixture.upload(t, ownerSession, "reject.png", "image/png", syntheticPNG(t, color.RGBA{R: 80, G: 40, B: 180, A: 255}))
	fixture.processNext(t)
	rejectReviewResponse := fixture.request(http.MethodGet, "/api/v1/reviews/"+secondUpload["job_id"], nil, ownerSession, false, "")
	assertStatus(t, rejectReviewResponse, http.StatusOK)
	rejectClaimSetID := asString(t, decodeMap(t, rejectReviewResponse)["claim_set_id"])
	rejectJSON := "{\"expected_revision\":1,\"reason\":\"不是账单\"}"
	reject := fixture.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+secondUpload["job_id"]+"/reject", strings.NewReader(rejectJSON), ownerSession, true, "application/json", map[string]string{"Idempotency-Key": "reject-0001"})
	assertStatus(t, reject, http.StatusNoContent)
	rejectReplay := fixture.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+secondUpload["job_id"]+"/reject", strings.NewReader(rejectJSON), ownerSession, true, "application/json", map[string]string{"Idempotency-Key": "reject-0001"})
	assertStatus(t, rejectReplay, http.StatusNoContent)

	cancelUpload := fixture.upload(t, ownerSession, "cancel.png", "image/png", syntheticPNG(t, color.RGBA{R: 180, G: 40, B: 80, A: 255}))
	assertStatus(t, fixture.request(http.MethodPost, "/api/v1/jobs/"+cancelUpload["job_id"]+"/cancel", nil, ownerSession, true, ""), http.StatusOK)
	retryUpload := fixture.upload(t, ownerSession, "retry.png", "image/png", syntheticPNG(t, color.RGBA{R: 180, G: 80, B: 40, A: 255}))
	if _, err := fixture.store.DB().Exec("UPDATE processing_jobs SET status = 'failed', error_code = 'provider_timeout', safe_error_message = '可重试' WHERE id = ?", retryUpload["job_id"]); err != nil {
		t.Fatal(err)
	}
	retried := fixture.request(http.MethodPost, "/api/v1/jobs/"+retryUpload["job_id"]+"/retry", nil, ownerSession, true, "")
	assertStatus(t, retried, http.StatusOK)
	if !strings.Contains(retried.Body.String(), "\"status\":\"queued\"") {
		t.Fatalf("retry did not requeue: %s", retried.Body.String())
	}

	secondTenantID := newID(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := fixture.store.DB().Exec("INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES (?, 'Second', 'CNY', 'Asia/Shanghai', ?, ?)", secondTenantID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec("INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES (?, ?, 'owner', 'active', ?, ?)", secondTenantID, fixture.owner.UserID, now, now); err != nil {
		t.Fatal(err)
	}
	secondTenantSession := fixture.login(t, secondTenantID)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/jobs/"+firstUpload["job_id"], nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"], nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"]+"/content", nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+firstUpload["document_id"]+"/pages/1/content", nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/claim-sets/"+asString(t, review["claim_set_id"]), nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/reviews/"+secondUpload["job_id"], nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodPost, "/api/v1/jobs/"+retryUpload["job_id"]+"/cancel", nil, secondTenantSession, true, ""), http.StatusNotFound)
	crossTenantConfirm := fixture.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+secondUpload["job_id"]+"/confirm", strings.NewReader("{\"expected_revision\":1,\"association_mode\":\"no_candidate\",\"allocations\":[],\"duplicate_resolutions\":[]}"), secondTenantSession, true, "application/json", map[string]string{"Idempotency-Key": "tenant-0001"})
	assertStatus(t, crossTenantConfirm, http.StatusNotFound)
	paymentID := asString(t, confirmed["fact_id"])
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/payments/"+paymentID, nil, secondTenantSession, true, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/provider-configs/"+providerID, nil, secondTenantSession, true, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/documents/"+secondUpload["document_id"], nil, secondTenantSession, true, ""), http.StatusNotFound)

	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/payments/"+paymentID, nil, ownerSession, true, ""), http.StatusNoContent)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/payments/"+paymentID, nil, ownerSession, true, ""), http.StatusNotFound)
	paymentsAfterDelete := fixture.request(http.MethodGet, "/api/v1/payments", nil, ownerSession, false, "")
	assertStatus(t, paymentsAfterDelete, http.StatusOK)
	if paymentsAfterDelete.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("deleted payment remained visible: %s", paymentsAfterDelete.Body.String())
	}
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/provider-configs/"+providerID, nil, ownerSession, true, ""), http.StatusNoContent)
	providersAfterDelete := fixture.request(http.MethodGet, "/api/v1/provider-configs", nil, ownerSession, false, "")
	assertStatus(t, providersAfterDelete, http.StatusOK)
	if providersAfterDelete.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("deleted provider remained visible: %s", providersAfterDelete.Body.String())
	}
	var encryptedKey []byte
	var active int
	var deletedAt string
	if err := fixture.store.DB().QueryRow(`
		SELECT encrypted_api_key, active, deleted_at
		FROM provider_configs WHERE tenant_id = ? AND id = ?
	`, fixture.owner.TenantID, providerID).Scan(&encryptedKey, &active, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if encryptedKey != nil || active != 0 || deletedAt == "" {
		t.Fatalf("provider deletion state = key:%v active:%d deleted:%q", encryptedKey, active, deletedAt)
	}
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/documents/"+firstUpload["document_id"], nil, ownerSession, true, ""), http.StatusConflict)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/documents/"+secondUpload["document_id"], nil, ownerSession, true, ""), http.StatusNoContent)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+secondUpload["document_id"], nil, ownerSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/documents/"+secondUpload["document_id"]+"/content", nil, ownerSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/jobs/"+secondUpload["job_id"], nil, ownerSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/claim-sets/"+rejectClaimSetID, nil, ownerSession, false, ""), http.StatusNotFound)
	var resourceHash, objectHashes, resourceCounts string
	if err := fixture.store.DB().QueryRow(`
		SELECT resource_id_hash, object_hashes_json, resource_counts_json
		FROM deletion_tombstones WHERE tenant_id = ? AND resource_type = 'document_aggregate'
	`, fixture.owner.TenantID).Scan(&resourceHash, &objectHashes, &resourceCounts); err != nil {
		t.Fatal(err)
	}
	if resourceHash == secondUpload["document_id"] || strings.Contains(objectHashes, "reject.png") || !strings.Contains(resourceCounts, "\"documents\":1") || !strings.Contains(resourceCounts, "\"document_pages\":1") {
		t.Fatalf("unsafe/incomplete tombstone = hash:%q objects:%s counts:%s", resourceHash, objectHashes, resourceCounts)
	}

	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/session", nil, ownerSession, true, ""), http.StatusNoContent)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/session", nil, ownerSession, false, ""), http.StatusUnauthorized)
}

func newHTTPTestFixture(t *testing.T) *httpTestFixture {
	t.Helper()
	root := t.TempDir()
	store, err := sqliteadapter.Open(context.Background(), sqliteadapter.Config{
		DatabasePath:  filepath.Join(root, "sbm.sqlite"),
		MigrationsDir: projectPath(t, "infra", "migrations"),
	})
	if err != nil {
		t.Fatal(err)
	}
	hasher := testPasswordHasher{}
	bootstrapService := bootstrap.NewService(store, hasher, system.IDGenerator{}, system.Clock{})
	ownerEmail := "owner@example.test"
	ownerPassword := "owner-password-123"
	owner, err := bootstrapService.Execute(context.Background(), bootstrap.Input{
		Email: ownerEmail, Password: []byte(ownerPassword), DisplayName: "Owner", TenantName: "Primary",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "Asia/Shanghai",
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := bootstrapService.Execute(context.Background(), bootstrap.Input{
		Email: "second@example.test", Password: []byte("second-password"), DisplayName: "Second", TenantName: "Second",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "Asia/Shanghai",
	}); !errors.Is(err, domain.ErrBootstrapNotEmpty) {
		store.Close()
		t.Fatalf("second bootstrap error = %v", err)
	}
	authService, err := auth.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{}, time.Hour)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	objects, err := localstorage.New(filepath.Join(root, "objects"))
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	normalizer, err := localstorage.NewNormalizer(objects, "/bin/false")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	cipher, err := cryptography.NewSecretCipher(bytes.Repeat([]byte{0x41}, 32))
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	providerService := providers.NewService(store, store, cipher, passingDetector{}, system.IDGenerator{}, system.Clock{})
	uploadService := documents.NewUploadService(objects, inspector, store, system.IDGenerator{}, system.Clock{})
	documentQueries := documents.NewQueryService(store, store, objects)
	jobActions := documents.NewActionService(store, store, system.IDGenerator{}, system.Clock{})
	documentDeletions := documents.NewDeletionService(store, objects, store, system.IDGenerator{}, system.Clock{})
	reviewService := reviews.NewService(store, store, system.IDGenerator{}, system.Clock{})
	factService := reviews.NewFactService(store, store, system.IDGenerator{}, system.Clock{})
	webRoot := filepath.Join(root, "web")
	if err := os.Mkdir(webRoot, 0o700); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<!doctype html><title>M1</title>"), 0o600); err != nil {
		store.Close()
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := NewServer(authService, uploadService, documentQueries, jobActions, documentDeletions, providerService, reviewService, factService, store, readyFixture{}, logger, Config{Version: "test", WebDistPath: webRoot})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	worker, err := processing.NewWorker(store, store, cipher, normalizer, objects, staticExtractor{}, store, system.IDGenerator{}, system.Clock{}, logger, processing.WorkerConfig{
		Concurrency: 2, PollInterval: 100 * time.Millisecond, JobTimeout: 150 * time.Second, LeaseDuration: 165 * time.Second,
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	return &httpTestFixture{store: store, handler: server.Handler(), worker: worker, owner: owner, ownerEmail: ownerEmail, ownerPassword: ownerPassword}
}

func projectPath(t *testing.T, parts ...string) string {
	t.Helper()
	location, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "..", "..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func (f *httpTestFixture) login(t *testing.T, tenantID string) *testSession {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"email": f.ownerEmail, "password": f.ownerPassword, "tenant_id": tenantID})
	if err != nil {
		t.Fatal(err)
	}
	response := f.request(http.MethodPost, "/api/v1/session/login", bytes.NewReader(payload), nil, false, "application/json")
	assertStatus(t, response, http.StatusOK)
	body := decodeMap(t, response)
	return &testSession{cookies: response.Result().Cookies(), csrf: asString(t, body["csrf_token"])}
}

func (f *httpTestFixture) request(method, path string, body io.Reader, session *testSession, csrf bool, contentType string) *httptest.ResponseRecorder {
	return f.requestWithHeaders(method, path, body, session, csrf, contentType, nil)
}

func (f *httpTestFixture) requestWithHeaders(method, path string, body io.Reader, session *testSession, csrf bool, contentType string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if session != nil {
		for _, cookie := range session.cookies {
			request.AddCookie(cookie)
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", session.csrf)
		}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func (f *httpTestFixture) upload(t *testing.T, session *testSession, name, mime string, content []byte) map[string]string {
	t.Helper()
	response := f.uploadRaw(t, session, name, mime, content)
	assertStatus(t, response, http.StatusCreated)
	body := decodeMap(t, response)
	return map[string]string{"document_id": asString(t, body["document_id"]), "job_id": asString(t, body["job_id"])}
}

func (f *httpTestFixture) uploadRaw(t *testing.T, session *testSession, name, mime string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf("form-data; name=\"file\"; filename=\"%s\"", name))
	header.Set("Content-Type", mime)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return f.request(http.MethodPost, "/api/v1/documents", &body, session, true, writer.FormDataContentType())
}

func (f *httpTestFixture) processNext(t *testing.T) {
	t.Helper()
	now := time.Now()
	job, err := f.store.LeaseNextJob(context.Background(), "http-test", now, now.Add(165*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.worker.ProcessOne(context.Background(), job); err != nil {
		t.Fatal(err)
	}
}

func revisionPayload(t *testing.T, review map[string]any) []byte {
	t.Helper()
	rawFields, ok := review["fields"].([]any)
	if !ok {
		t.Fatalf("review fields = %T", review["fields"])
	}
	fields := make([]map[string]any, 0, len(rawFields))
	for _, raw := range rawFields {
		field, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("review field = %T", raw)
		}
		if field["path"] == "document_type" {
			continue
		}
		item := map[string]any{"path": field["path"], "value_type": field["value_type"], "presence": field["presence"]}
		if field["presence"] == "present" {
			item["value"] = field["value"]
			rawEvidence, _ := field["evidence"].([]any)
			evidenceIDs := make([]string, 0, len(rawEvidence))
			for _, rawItem := range rawEvidence {
				evidence := rawItem.(map[string]any)
				evidenceIDs = append(evidenceIDs, asString(t, evidence["id"]))
			}
			item["evidence_ids"] = evidenceIDs
		}
		fields = append(fields, item)
	}
	payload, err := json.Marshal(map[string]any{
		"expected_revision": review["revision"], "expected_optimistic_version": review["optimistic_version"],
		"document_type": review["document_type"], "fields": fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func syntheticPNG(t *testing.T, fill color.RGBA) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			canvas.SetRGBA(x, y, fill)
		}
	}
	var content bytes.Buffer
	if err := png.Encode(&content, canvas); err != nil {
		t.Fatal(err)
	}
	return content.Bytes()
}

func assertStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, want, response.Body.String())
	}
}

func decodeMap(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var target map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &target); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, response.Body.String())
	}
	return target
}

func asString(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok || result == "" {
		t.Fatalf("value is not a non-empty string: %#v", value)
	}
	return result
}

func asInt(t *testing.T, value any) int {
	t.Helper()
	result, ok := value.(float64)
	if !ok {
		t.Fatalf("value is not a JSON number: %#v", value)
	}
	return int(result)
}

func newID(t *testing.T) string {
	t.Helper()
	id, err := (system.IDGenerator{}).NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
