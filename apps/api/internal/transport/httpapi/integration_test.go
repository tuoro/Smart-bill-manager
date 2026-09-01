package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
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
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/emailmime"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/bootstrap"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	applicationemails "github.com/tuoro/smart-bill-manager/apps/api/internal/application/emails"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/insights"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/processing"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/providers"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

type httpTestFixture struct {
	store         *postgresqladapter.Store
	handler       http.Handler
	worker        *processing.Worker
	owner         bootstrap.Result
	ownerEmail    string
	ownerPassword string
	emailArchive  applicationemails.Service
}

type testSession struct {
	cookies []*http.Cookie
	csrf    string
}

type passingDetector struct{}

func (passingDetector) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/2", SHA256: strings.Repeat("c", 64)}
}

func (passingDetector) DetectCapabilities(context.Context, ports.ProviderCredentials) ports.CapabilityResult {
	return ports.CapabilityResult{Passed: true, SafeMessage: "能力检测通过"}
}

type staticExtractor struct{}

func (staticExtractor) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/2", SHA256: strings.Repeat("c", 64)}
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
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/2", SHA256: strings.Repeat("c", 64)}
}

func (staticPreparedExtraction) Execute(context.Context) (ports.BillExtractionResult, error) {
	return ports.BillExtractionResult{
		Envelope: domain.BillVisibleTextEnvelope{
			SchemaVersion: "bill-visible-text/2",
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
	if detectedProvider["capability_schema_version"] != "bill-visible-text-provider/2" ||
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
	if runPromptVersion != "bill-visible-text-cn/2" || runExtractionSchemaVersion != "bill-visible-text/2" ||
		runProviderSchemaVersion != "bill-visible-text-provider/2" || runProviderSchemaSHA256 != strings.Repeat("c", 64) ||
		runClaimMapperVersion != "claim-mapper/4" {
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
	nullAssociationShape := fixture.requestWithHeaders(
		http.MethodPost,
		"/api/v1/reviews/"+firstUpload["job_id"]+"/confirm",
		strings.NewReader("{\"expected_revision\":2,\"association_mode\":null,\"allocations\":null,\"duplicate_resolutions\":[]}"),
		ownerSession,
		true,
		"application/json",
		map[string]string{"Idempotency-Key": "null-association-shape"},
	)
	assertStatus(t, nullAssociationShape, http.StatusBadRequest)
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
	paymentID := asString(t, confirmed["fact_id"])
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
	allocationWorkspaceResponse := fixture.request(http.MethodGet, "/api/v1/allocations/payment/"+paymentID, nil, ownerSession, false, "")
	assertStatus(t, allocationWorkspaceResponse, http.StatusOK)
	allocationWorkspace := decodeMap(t, allocationWorkspaceResponse)
	planHash := asString(t, allocationWorkspace["plan_hash"])
	if len(planHash) != 64 || len(allocationWorkspace["links"].([]any)) != 0 || len(allocationWorkspace["targets"].([]any)) != 0 {
		t.Fatalf("empty allocation workspace = %#v", allocationWorkspace)
	}
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/allocations/unknown/"+paymentID, nil, ownerSession, false, ""), http.StatusBadRequest)
	missingPlan := `{"expected_plan_hash":"` + planHash + `","reason":"缺少完整数组"}`
	assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/allocations/payment/"+paymentID+"/adjustments", strings.NewReader(missingPlan), ownerSession, true, "application/json", map[string]string{"Idempotency-Key": "allocation-http-missing"}), http.StatusBadRequest)
	unknownField := `{"expected_plan_hash":"` + planHash + `","desired_allocations":[],"reason":"严格 JSON","unknown":true}`
	assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/allocations/payment/"+paymentID+"/adjustments", strings.NewReader(unknownField), ownerSession, true, "application/json", map[string]string{"Idempotency-Key": "allocation-http-unknown"}), http.StatusBadRequest)
	emptyPlan := `{"expected_plan_hash":"` + planHash + `","desired_allocations":[],"reason":"没有变化"}`
	assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/allocations/payment/"+paymentID+"/adjustments", strings.NewReader(emptyPlan), ownerSession, false, "application/json", map[string]string{"Idempotency-Key": "allocation-http-no-csrf"}), http.StatusForbidden)
	assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/allocations/payment/"+paymentID+"/adjustments", strings.NewReader(emptyPlan), ownerSession, true, "application/json", map[string]string{"Idempotency-Key": "allocation-http-unchanged"}), http.StatusConflict)

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
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/allocations/payment/"+paymentID, nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/payments/"+paymentID, nil, secondTenantSession, true, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/provider-configs/"+providerID, nil, secondTenantSession, true, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/documents/"+secondUpload["document_id"], nil, secondTenantSession, true, ""), http.StatusNotFound)

	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/payments/"+paymentID, nil, ownerSession, true, ""), http.StatusNoContent)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/allocations/payment/"+paymentID, nil, ownerSession, false, ""), http.StatusNotFound)
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
	var active bool
	var deletedAt string
	if err := fixture.store.DB().QueryRow(`
		SELECT encrypted_api_key, active, deleted_at
		FROM provider_configs WHERE tenant_id = ? AND id = ?
	`, fixture.owner.TenantID, providerID).Scan(&encryptedKey, &active, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if encryptedKey != nil || active || deletedAt == "" {
		t.Fatalf("provider deletion state = key:%v active:%t deleted:%q", encryptedKey, active, deletedAt)
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
	var tombstoneCounts map[string]int
	if err := json.Unmarshal([]byte(resourceCounts), &tombstoneCounts); err != nil {
		t.Fatal(err)
	}
	if resourceHash == secondUpload["document_id"] || strings.Contains(objectHashes, "reject.png") || tombstoneCounts["documents"] != 1 || tombstoneCounts["document_pages"] != 1 {
		t.Fatalf("unsafe/incomplete tombstone = hash:%q objects:%s counts:%s", resourceHash, objectHashes, resourceCounts)
	}

	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/session", nil, ownerSession, true, ""), http.StatusNoContent)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/session", nil, ownerSession, false, ""), http.StatusUnauthorized)
}

func TestHTTPEmailArchiveReadAndRegistrationBoundaries(t *testing.T) {
	fixture := newHTTPTestFixture(t)
	defer fixture.store.Close()
	ownerSession := fixture.login(t, fixture.owner.TenantID)
	registration := `{"display_name":" 合成财务邮箱 ","mailbox_address":"Finance@Example.Invalid","imap_host":"IMAP.EXAMPLE.INVALID","imap_port":993,"transport_security":"implicit_tls"}`
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/email-sources", strings.NewReader(registration), ownerSession, false,
		"application/json", map[string]string{"Idempotency-Key": "http-email-source"},
	), http.StatusForbidden)
	assertStatus(t, fixture.request(
		http.MethodPost, "/api/v1/email-sources", strings.NewReader(registration), ownerSession, true, "application/json",
	), http.StatusBadRequest)
	unknownField := strings.TrimSuffix(registration, "}") + `,"password":"must-not-exist"}`
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/email-sources", strings.NewReader(unknownField), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "http-email-unknown"},
	), http.StatusBadRequest)
	createdResponse := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/email-sources", strings.NewReader(registration), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "http-email-source"},
	)
	assertStatus(t, createdResponse, http.StatusCreated)
	created := decodeMap(t, createdResponse)
	sourceID := asString(t, created["id"])
	if created["display_name"] != "合成财务邮箱" || created["mailbox_address"] != "finance@example.invalid" ||
		created["imap_host"] != "imap.example.invalid" || created["status"] != domain.EmailSourcePendingConnection {
		t.Fatalf("created source = %#v", created)
	}
	if bytes.Contains(createdResponse.Body.Bytes(), []byte("password")) || bytes.Contains(createdResponse.Body.Bytes(), []byte("token")) {
		t.Fatalf("source response exposed credential-shaped fields: %s", createdResponse.Body.String())
	}
	replayResponse := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/email-sources", strings.NewReader(registration), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "http-email-source"},
	)
	assertStatus(t, replayResponse, http.StatusOK)
	if asString(t, decodeMap(t, replayResponse)["id"]) != sourceID {
		t.Fatal("source replay changed identity")
	}
	changed := strings.Replace(registration, "合成财务邮箱", "另一个邮箱", 1)
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/email-sources", strings.NewReader(changed), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "http-email-source"},
	), http.StatusConflict)

	png := syntheticPNG(t, color.RGBA{R: 22, G: 44, B: 66, A: 255})
	raw := []byte(strings.Join([]string{
		"From: sender@example.invalid",
		"Subject: 合成附件邮件",
		"Content-Type: multipart/mixed; boundary=http-email",
		"",
		"--http-email",
		"Content-Type: text/plain",
		"",
		"private body marker",
		"--http-email",
		"Content-Type: image/png; name=invoice.png",
		"Content-Disposition: attachment; filename=invoice.png",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString(png),
		"--http-email--",
		"",
	}, "\r\n"))
	archived, err := fixture.emailArchive.Archive(context.Background(), applicationemails.ArchiveInput{
		TenantID: fixture.owner.TenantID, EmailSourceID: sourceID,
		ExternalMessageKey: strings.Repeat("1", 64), ReceivedAt: time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC),
		Raw: bytes.NewReader(raw), RequestID: "http-email-archive",
	})
	if err != nil || len(archived.Attachments) != 1 {
		t.Fatalf("archive fixture = %#v, error=%v", archived, err)
	}
	blocked, err := fixture.emailArchive.Archive(context.Background(), applicationemails.ArchiveInput{
		TenantID: fixture.owner.TenantID, EmailSourceID: sourceID,
		ExternalMessageKey: strings.Repeat("2", 64), ReceivedAt: time.Date(2026, 8, 31, 3, 0, 0, 0, time.UTC),
		Raw: strings.NewReader("not a mail header"), RequestID: "http-email-blocked",
	})
	if err != nil || blocked.Status != domain.EmailMessageBlocked {
		t.Fatalf("blocked fixture = %#v, error=%v", blocked, err)
	}

	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-sources", nil, nil, false, ""), http.StatusUnauthorized)
	sourceListResponse := fixture.request(http.MethodGet, "/api/v1/email-sources", nil, ownerSession, false, "")
	assertStatus(t, sourceListResponse, http.StatusOK)
	sourceList := decodeMap(t, sourceListResponse)["items"].([]any)
	if len(sourceList) != 1 || sourceList[0].(map[string]any)["status"] != domain.EmailSourceActive ||
		asInt(t, sourceList[0].(map[string]any)["message_count"]) != 2 ||
		asInt(t, sourceList[0].(map[string]any)["blocked_count"]) != 1 {
		t.Fatalf("source list = %#v", sourceList)
	}
	firstPageResponse := fixture.request(http.MethodGet, "/api/v1/email-sources/"+sourceID+"/messages?limit=1", nil, ownerSession, false, "")
	assertStatus(t, firstPageResponse, http.StatusOK)
	firstPage := decodeMap(t, firstPageResponse)
	if len(firstPage["items"].([]any)) != 1 || firstPage["next_cursor"] == "" {
		t.Fatalf("first email page = %#v", firstPage)
	}
	cursor := asString(t, firstPage["next_cursor"])
	secondPageResponse := fixture.request(http.MethodGet, "/api/v1/email-sources/"+sourceID+"/messages?limit=1&cursor="+cursor, nil, ownerSession, false, "")
	assertStatus(t, secondPageResponse, http.StatusOK)
	if bytes.Contains(secondPageResponse.Body.Bytes(), []byte("private body marker")) ||
		bytes.Contains(secondPageResponse.Body.Bytes(), []byte("external_message_key")) ||
		bytes.Contains(secondPageResponse.Body.Bytes(), []byte("raw_sha256")) ||
		bytes.Contains(secondPageResponse.Body.Bytes(), []byte("storage_key")) {
		t.Fatalf("message projection exposed private internals: %s", secondPageResponse.Body.String())
	}
	secondPage := decodeMap(t, secondPageResponse)
	message := secondPage["items"].([]any)[0].(map[string]any)
	attachment := message["attachments"].([]any)[0].(map[string]any)
	attachmentID := asString(t, attachment["id"])
	if attachment["processing_status"] != domain.EmailAttachmentQueued || attachment["document_id"] == "" {
		t.Fatalf("attachment projection = %#v", attachment)
	}
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-sources/"+sourceID+"/messages?cursor=%25%25%25", nil, ownerSession, false, ""), http.StatusBadRequest)

	rawDownload := fixture.request(http.MethodGet, "/api/v1/email-messages/"+archived.MessageID+"/raw", nil, ownerSession, false, "")
	assertStatus(t, rawDownload, http.StatusOK)
	assertArchiveDownload(t, rawDownload, "message/rfc822")
	if !bytes.Equal(rawDownload.Body.Bytes(), raw) {
		t.Fatal("raw email download changed bytes")
	}
	attachmentDownload := fixture.request(http.MethodGet, "/api/v1/email-attachments/"+attachmentID+"/content", nil, ownerSession, false, "")
	assertStatus(t, attachmentDownload, http.StatusOK)
	assertArchiveDownload(t, attachmentDownload, "image/png")
	if !bytes.Equal(attachmentDownload.Body.Bytes(), png) {
		t.Fatal("email attachment download changed bytes")
	}

	financeSession := fixture.addRoleSession(t, domain.RoleFinance)
	reviewerSession := fixture.addRoleSession(t, domain.RoleReviewer)
	viewerSession := fixture.addRoleSession(t, domain.RoleViewer)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-sources", nil, financeSession, false, ""), http.StatusOK)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-messages/"+archived.MessageID+"/raw", nil, financeSession, false, ""), http.StatusOK)
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/email-sources", strings.NewReader(registration), financeSession, true,
		"application/json", map[string]string{"Idempotency-Key": "finance-email-source"},
	), http.StatusForbidden)
	for _, denied := range []*testSession{reviewerSession, viewerSession} {
		assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-sources", nil, denied, false, ""), http.StatusForbidden)
		assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-messages/"+archived.MessageID+"/raw", nil, denied, false, ""), http.StatusForbidden)
	}

	secondTenantID := newID(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := fixture.store.DB().Exec("INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES (?, 'Other', 'CNY', 'UTC', ?, ?)", secondTenantID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec("INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES (?, ?, 'owner', 'active', ?, ?)", secondTenantID, fixture.owner.UserID, now, now); err != nil {
		t.Fatal(err)
	}
	secondTenantSession := fixture.login(t, secondTenantID)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-sources/"+sourceID+"/messages", nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-messages/"+archived.MessageID+"/raw", nil, secondTenantSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/email-attachments/"+attachmentID+"/content", nil, secondTenantSession, false, ""), http.StatusNotFound)
	internalWriteRoute := fixture.request(http.MethodPost, "/api/v1/email-messages", strings.NewReader("{}"), ownerSession, true, "application/json")
	if internalWriteRoute.Code != http.StatusMethodNotAllowed && internalWriteRoute.Code != http.StatusNotFound {
		t.Fatalf("unexpected public email archive route status = %d", internalWriteRoute.Code)
	}
}

func TestHTTPTripAttributionContractAndPermissionBoundaries(t *testing.T) {
	fixture := newHTTPTestFixture(t)
	defer fixture.store.Close()

	ownerSession := fixture.login(t, fixture.owner.TenantID)
	financeSession := fixture.addRoleSession(t, domain.RoleFinance)
	reviewerSession := fixture.addRoleSession(t, domain.RoleReviewer)
	viewerSession := fixture.addRoleSession(t, domain.RoleViewer)
	tripID := newID(t)
	factID := newID(t)
	validAssignment := fmt.Sprintf(
		`{"fact_type":"payment","fact_id":%q,"desired_trip_id":null,"expected_assignment_id":null,"reason":"合成归属边界"}`,
		factID,
	)

	for _, readable := range []*testSession{ownerSession, financeSession, viewerSession} {
		response := fixture.request(http.MethodGet, "/api/v1/trips", nil, readable, false, "")
		assertStatus(t, response, http.StatusOK)
		if response.Body.String() != "{\"items\":[]}\n" {
			t.Fatalf("empty Trip list = %s", response.Body.String())
		}
	}
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trips", nil, reviewerSession, false, ""), http.StatusForbidden)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trips/"+tripID+"/attribution-candidates?view=all&limit=20", nil, ownerSession, false, ""), http.StatusNotFound)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trips/"+tripID+"/attribution-candidates?view=invalid", nil, ownerSession, false, ""), http.StatusBadRequest)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trips/"+tripID+"/attribution-candidates?limit=101", nil, ownerSession, false, ""), http.StatusBadRequest)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trips/"+tripID+"/attribution-candidates?cursor=%25%25%25", nil, ownerSession, false, ""), http.StatusBadRequest)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trips/"+tripID+"/attribution-candidates", nil, reviewerSession, false, ""), http.StatusForbidden)

	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/trip-assignments", strings.NewReader(validAssignment), ownerSession, false,
		"application/json", map[string]string{"Idempotency-Key": "trip-http-no-csrf"},
	), http.StatusForbidden)
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/trip-assignments", strings.NewReader(`{"fact_type":"payment","fact_id":"`+factID+`","expected_assignment_id":null,"reason":"缺少目标"}`), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "trip-http-missing-target"},
	), http.StatusBadRequest)
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/trip-assignments", strings.NewReader(validAssignment[:len(validAssignment)-1]+`,"unknown":true}`), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "trip-http-unknown"},
	), http.StatusBadRequest)
	for _, manager := range []*testSession{ownerSession, financeSession} {
		assertStatus(t, fixture.requestWithHeaders(
			http.MethodPost, "/api/v1/trip-assignments", strings.NewReader(validAssignment), manager, true,
			"application/json", map[string]string{"Idempotency-Key": "trip-http-valid-" + manager.csrf},
		), http.StatusNotFound)
	}
	for _, denied := range []*testSession{reviewerSession, viewerSession} {
		assertStatus(t, fixture.requestWithHeaders(
			http.MethodPost, "/api/v1/trip-assignments", strings.NewReader(validAssignment), denied, true,
			"application/json", map[string]string{"Idempotency-Key": "trip-http-denied-" + denied.csrf},
		), http.StatusForbidden)
	}
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/trips/"+tripID, nil, financeSession, true, ""), http.StatusForbidden)
	assertStatus(t, fixture.request(http.MethodDelete, "/api/v1/trips/"+tripID, nil, ownerSession, true, ""), http.StatusNotFound)
}

func TestHTTPReimbursementContractAndPermissionBoundaries(t *testing.T) {
	fixture := newHTTPTestFixture(t)
	defer fixture.store.Close()

	ownerSession := fixture.login(t, fixture.owner.TenantID)
	financeSession := fixture.addRoleSession(t, domain.RoleFinance)
	reviewerSession := fixture.addRoleSession(t, domain.RoleReviewer)
	viewerSession := fixture.addRoleSession(t, domain.RoleViewer)
	tripID := newID(t)
	assignmentID := newID(t)
	reimbursementID := newID(t)
	preview := fmt.Sprintf(`{"trip_id":%q,"assignment_ids":[%q]}`, tripID, assignmentID)
	submission := fmt.Sprintf(
		`{"trip_id":%q,"assignment_ids":[%q],"expected_snapshot_hash":%q,"acknowledged_finding_keys":[],"reason":"合成报销提交"}`,
		tripID, assignmentID, strings.Repeat("a", 64),
	)
	statusDecision := `{"expected_status":"submitted","desired_status":"reimbursed","expected_version":1,"reason":"合成状态变化"}`

	for _, readable := range []*testSession{ownerSession, financeSession, viewerSession} {
		response := fixture.request(http.MethodGet, "/api/v1/reimbursements", nil, readable, false, "")
		assertStatus(t, response, http.StatusOK)
		if response.Body.String() != "{\"items\":[]}\n" {
			t.Fatalf("empty reimbursement list = %s", response.Body.String())
		}
		assertStatus(t, fixture.request(
			http.MethodGet, "/api/v1/reimbursements/"+reimbursementID, nil, readable, false, "",
		), http.StatusNotFound)
	}
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/reimbursements", nil, reviewerSession, false, ""), http.StatusForbidden)
	assertStatus(t, fixture.request(
		http.MethodGet, "/api/v1/reimbursements/"+reimbursementID, nil, reviewerSession, false, "",
	), http.StatusForbidden)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/reimbursements?limit=101", nil, ownerSession, false, ""), http.StatusBadRequest)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/reimbursements?cursor=%25%25%25", nil, ownerSession, false, ""), http.StatusBadRequest)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/reimbursements/not-a-uuid", nil, ownerSession, false, ""), http.StatusBadRequest)

	assertStatus(t, fixture.request(
		http.MethodPost, "/api/v1/reimbursement-previews", strings.NewReader(preview), ownerSession, false, "application/json",
	), http.StatusForbidden)
	assertStatus(t, fixture.request(
		http.MethodPost, "/api/v1/reimbursement-previews", strings.NewReader(preview[:len(preview)-1]+`,"unknown":true}`), ownerSession, true, "application/json",
	), http.StatusBadRequest)
	assertStatus(t, fixture.request(
		http.MethodPost, "/api/v1/reimbursement-previews", strings.NewReader(`{"trip_id":"not-a-uuid","assignment_ids":["also-invalid"]}`), ownerSession, true, "application/json",
	), http.StatusBadRequest)
	for _, manager := range []*testSession{ownerSession, financeSession} {
		assertStatus(t, fixture.request(
			http.MethodPost, "/api/v1/reimbursement-previews", strings.NewReader(preview), manager, true, "application/json",
		), http.StatusNotFound)
	}
	for _, denied := range []*testSession{reviewerSession, viewerSession} {
		assertStatus(t, fixture.request(
			http.MethodPost, "/api/v1/reimbursement-previews", strings.NewReader(preview), denied, true, "application/json",
		), http.StatusForbidden)
	}

	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements", strings.NewReader(submission), ownerSession, false,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-no-csrf"},
	), http.StatusForbidden)
	for _, manager := range []*testSession{ownerSession, financeSession} {
		assertStatus(t, fixture.requestWithHeaders(
			http.MethodPost, "/api/v1/reimbursements", strings.NewReader(submission), manager, true,
			"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-submit-" + manager.csrf},
		), http.StatusNotFound)
		assertStatus(t, fixture.requestWithHeaders(
			http.MethodPost, "/api/v1/reimbursements/"+reimbursementID+"/status-decisions", strings.NewReader(statusDecision), manager, true,
			"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-status-" + manager.csrf},
		), http.StatusNotFound)
	}
	for _, denied := range []*testSession{reviewerSession, viewerSession} {
		assertStatus(t, fixture.requestWithHeaders(
			http.MethodPost, "/api/v1/reimbursements", strings.NewReader(submission), denied, true,
			"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-denied-" + denied.csrf},
		), http.StatusForbidden)
		assertStatus(t, fixture.requestWithHeaders(
			http.MethodPost, "/api/v1/reimbursements/"+reimbursementID+"/status-decisions", strings.NewReader(statusDecision), denied, true,
			"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-status-denied-" + denied.csrf},
		), http.StatusForbidden)
	}
}

func TestHTTPInsightQueryContractAndPermissionBoundaries(t *testing.T) {
	fixture := newHTTPTestFixture(t)
	defer fixture.store.Close()
	ownerSession := fixture.login(t, fixture.owner.TenantID)
	financeSession := fixture.addRoleSession(t, domain.RoleFinance)
	reviewerSession := fixture.addRoleSession(t, domain.RoleReviewer)
	viewerSession := fixture.addRoleSession(t, domain.RoleViewer)

	for _, readable := range []*testSession{ownerSession, financeSession, viewerSession} {
		response := fixture.request(http.MethodGet, "/api/v1/insights", nil, readable, false, "")
		assertStatus(t, response, http.StatusOK)
		body := decodeMap(t, response)
		if body["rule_version"] != domain.InsightRuleVersion {
			t.Fatalf("insight rule version = %#v", body["rule_version"])
		}
		items, itemsOK := body["items"].([]any)
		groups, groupsOK := body["groups"].([]any)
		filter, filterOK := body["filter"].(map[string]any)
		if !itemsOK || len(items) != 0 || !groupsOK || len(groups) != 0 || !filterOK ||
			filter["fact_type"] != domain.InsightFactTypeAll ||
			filter["allocation_status"] != domain.InsightStatusAll ||
			filter["trip_scope"] != domain.InsightTripScopeAll {
			t.Fatalf("empty insight response = %#v", body)
		}
	}
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/insights", nil, reviewerSession, false, ""), http.StatusForbidden)

	valid := "/api/v1/insights?fact_type=payment&date_from=2026-08-01&date_to=2026-08-31&currency=CNY&allocation_status=partial&trip_scope=unassigned&limit=100"
	assertStatus(t, fixture.request(http.MethodGet, valid, nil, ownerSession, false, ""), http.StatusOK)

	invalidQueries := []string{
		"?unknown=value",
		"?fact_type=payment&fact_type=invoice",
		"?fact_type=",
		"?fact_type=%20payment",
		"?date_from=2026-08-01",
		"?date_from=2026-08-31&date_to=2026-08-01",
		"?currency=GBP",
		"?allocation_status=unknown",
		"?trip_scope=current",
		"?trip_scope=unassigned&trip_id=11111111-1111-4111-8111-111111111111",
		"?trip_scope=assigned&trip_id=not-a-uuid",
		"?limit=0",
		"?limit=101",
		"?cursor=not-base64",
	}
	for _, query := range invalidQueries {
		response := fixture.request(http.MethodGet, "/api/v1/insights"+query, nil, ownerSession, false, "")
		assertStatus(t, response, http.StatusBadRequest)
	}
	notFound := fixture.request(
		http.MethodGet,
		"/api/v1/insights?trip_scope=assigned&trip_id=11111111-1111-4111-8111-111111111111",
		nil,
		ownerSession,
		false,
		"",
	)
	assertStatus(t, notFound, http.StatusNotFound)
}

func TestHTTPReimbursementSuccessfulLifecycleAndTenantIsolation(t *testing.T) {
	fixture := newHTTPTestFixture(t)
	defer fixture.store.Close()

	ownerSession := fixture.login(t, fixture.owner.TenantID)
	financeSession := fixture.addRoleSession(t, domain.RoleFinance)
	viewerSession := fixture.addRoleSession(t, domain.RoleViewer)
	activateHTTPTestProvider(t, fixture, ownerSession)

	paymentReview := processHTTPTestReview(
		t, fixture, ownerSession, "reimbursement-payment.png", color.RGBA{R: 20, G: 90, B: 170, A: 255},
	)
	paymentConfirm := fixture.requestWithHeaders(
		http.MethodPost,
		"/api/v1/reviews/"+asString(t, paymentReview["job"].(map[string]any)["id"])+"/confirm",
		bytes.NewReader(httpConfirmPayload(t, paymentReview, true)),
		ownerSession,
		true,
		"application/json",
		map[string]string{"Idempotency-Key": "reimbursement-http-confirm-payment"},
	)
	assertStatus(t, paymentConfirm, http.StatusOK)
	paymentID := asString(t, decodeMap(t, paymentConfirm)["fact_id"])

	tripReview := processHTTPTestReview(
		t, fixture, ownerSession, "reimbursement-trip.png", color.RGBA{R: 180, G: 70, B: 25, A: 255},
	)
	tripJobID := asString(t, tripReview["job"].(map[string]any)["id"])
	revisedTrip := fixture.request(
		http.MethodPost,
		"/api/v1/reviews/"+tripJobID+"/revisions",
		bytes.NewReader(httpTripRevisionPayload(t, tripReview)),
		ownerSession,
		true,
		"application/json",
	)
	assertStatus(t, revisedTrip, http.StatusCreated)
	tripReview = decodeMap(t, revisedTrip)
	tripConfirm := fixture.requestWithHeaders(
		http.MethodPost,
		"/api/v1/reviews/"+tripJobID+"/confirm",
		bytes.NewReader(httpConfirmPayload(t, tripReview, false)),
		ownerSession,
		true,
		"application/json",
		map[string]string{"Idempotency-Key": "reimbursement-http-confirm-trip"},
	)
	assertStatus(t, tripConfirm, http.StatusOK)
	tripID := asString(t, decodeMap(t, tripConfirm)["fact_id"])

	assignmentPayload, err := json.Marshal(map[string]any{
		"fact_type": "payment", "fact_id": paymentID, "desired_trip_id": tripID,
		"expected_assignment_id": nil, "reason": "合成 HTTP 报销归属",
	})
	if err != nil {
		t.Fatal(err)
	}
	assignmentResponse := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/trip-assignments", bytes.NewReader(assignmentPayload), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-assignment"},
	)
	assertStatus(t, assignmentResponse, http.StatusOK)
	assignmentID := asString(t, decodeMap(t, assignmentResponse)["assignment_id"])

	previewPayload, err := json.Marshal(map[string]any{"trip_id": tripID, "assignment_ids": []string{assignmentID}})
	if err != nil {
		t.Fatal(err)
	}
	previewResponse := fixture.request(
		http.MethodPost, "/api/v1/reimbursement-previews", bytes.NewReader(previewPayload), ownerSession, true, "application/json",
	)
	assertStatus(t, previewResponse, http.StatusOK)
	preview := decodeMap(t, previewResponse)
	findings, ok := preview["findings"].([]any)
	if !ok || len(findings) != 1 || findings[0].(map[string]any)["code"] != domain.ReimbursementFindingMissingInvoice {
		t.Fatalf("HTTP reimbursement preview findings = %#v", preview["findings"])
	}
	totals := preview["totals_by_currency"].([]any)
	if len(totals) != 1 || totals[0].(map[string]any)["currency"] != "CNY" || asInt(t, totals[0].(map[string]any)["amount_minor"]) != 1234 {
		t.Fatalf("HTTP reimbursement preview totals = %#v", totals)
	}
	findingKey := asString(t, findings[0].(map[string]any)["finding_key"])
	snapshotHash := asString(t, preview["snapshot_hash"])
	submissionBody := map[string]any{
		"trip_id": tripID, "assignment_ids": []string{assignmentID},
		"expected_snapshot_hash": snapshotHash, "acknowledged_finding_keys": []string{findingKey},
		"reason": "合成 HTTP 报销提交",
	}
	unknownSubmission := make(map[string]any, len(submissionBody)+1)
	for key, value := range submissionBody {
		unknownSubmission[key] = value
	}
	unknownSubmission["unknown"] = true
	unknownEncoded, err := json.Marshal(unknownSubmission)
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(unknownEncoded), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-submit-unknown"},
	), http.StatusBadRequest)

	unacknowledged := make(map[string]any, len(submissionBody))
	for key, value := range submissionBody {
		unacknowledged[key] = value
	}
	unacknowledged["acknowledged_finding_keys"] = []string{}
	unacknowledgedEncoded, err := json.Marshal(unacknowledged)
	if err != nil {
		t.Fatal(err)
	}
	unacknowledgedResponse := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(unacknowledgedEncoded), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-submit-unacknowledged"},
	)
	assertStatus(t, unacknowledgedResponse, http.StatusConflict)
	for _, privateValue := range []string{tripID, paymentID, assignmentID, "1234", "合成 HTTP 报销提交"} {
		if strings.Contains(unacknowledgedResponse.Body.String(), privateValue) {
			t.Fatalf("reimbursement error exposed private value %q: %s", privateValue, unacknowledgedResponse.Body.String())
		}
	}

	submissionEncoded, err := json.Marshal(submissionBody)
	if err != nil {
		t.Fatal(err)
	}
	createdResponse := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(submissionEncoded), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-submit-success"},
	)
	assertStatus(t, createdResponse, http.StatusCreated)
	created := decodeMap(t, createdResponse)
	reimbursementID := asString(t, created["reimbursement_id"])
	if created["status"] != "submitted" || asInt(t, created["version"]) != 1 || created["replayed"] != false {
		t.Fatalf("HTTP reimbursement creation = %#v", created)
	}
	replayResponse := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(submissionEncoded), ownerSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-submit-success"},
	)
	assertStatus(t, replayResponse, http.StatusOK)
	if replay := decodeMap(t, replayResponse); replay["reimbursement_id"] != reimbursementID || replay["replayed"] != true {
		t.Fatalf("HTTP reimbursement replay = %#v", replay)
	}

	listResponse := fixture.request(http.MethodGet, "/api/v1/reimbursements?limit=1", nil, viewerSession, false, "")
	assertStatus(t, listResponse, http.StatusOK)
	listed := decodeMap(t, listResponse)["items"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["id"] != reimbursementID {
		t.Fatalf("HTTP reimbursement list = %#v", listed)
	}
	detailResponse := fixture.request(http.MethodGet, "/api/v1/reimbursements/"+reimbursementID, nil, viewerSession, false, "")
	assertStatus(t, detailResponse, http.StatusOK)
	detail := decodeMap(t, detailResponse)
	if detail["status"] != "submitted" || asInt(t, detail["version"]) != 1 ||
		len(detail["items"].([]any)) != 1 || len(detail["findings"].([]any)) != 1 || len(detail["decisions"].([]any)) != 1 {
		t.Fatalf("HTTP reimbursement detail = %#v", detail)
	}

	unknownStatus := []byte(`{"expected_status":"submitted","desired_status":"reimbursed","expected_version":1,"reason":"合成状态变化","unknown":true}`)
	assertStatus(t, fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements/"+reimbursementID+"/status-decisions", bytes.NewReader(unknownStatus), financeSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-status-unknown"},
	), http.StatusBadRequest)
	statusBody := []byte(`{"expected_status":"submitted","desired_status":"reimbursed","expected_version":1,"reason":"合成财务完成"}`)
	statusResponse := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements/"+reimbursementID+"/status-decisions", bytes.NewReader(statusBody), financeSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-status-success"},
	)
	assertStatus(t, statusResponse, http.StatusOK)
	statusResult := decodeMap(t, statusResponse)
	if statusResult["status"] != "reimbursed" || asInt(t, statusResult["version"]) != 2 {
		t.Fatalf("HTTP reimbursement status = %#v", statusResult)
	}
	finalDetail := fixture.request(http.MethodGet, "/api/v1/reimbursements/"+reimbursementID, nil, ownerSession, false, "")
	assertStatus(t, finalDetail, http.StatusOK)
	if decoded := decodeMap(t, finalDetail); decoded["status"] != "reimbursed" || len(decoded["decisions"].([]any)) != 2 {
		t.Fatalf("HTTP final reimbursement detail = %#v", decoded)
	}

	secondTenantID := newID(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := fixture.store.DB().Exec(
		"INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES (?, 'Second', 'CNY', 'Asia/Shanghai', ?, ?)",
		secondTenantID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec(
		"INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES (?, ?, 'owner', 'active', ?, ?)",
		secondTenantID, fixture.owner.UserID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	secondTenantSession := fixture.login(t, secondTenantID)
	assertStatus(t, fixture.request(
		http.MethodGet, "/api/v1/reimbursements/"+reimbursementID, nil, secondTenantSession, false, "",
	), http.StatusNotFound)
	assertStatus(t, fixture.request(
		http.MethodPost, "/api/v1/reimbursement-previews", bytes.NewReader(previewPayload), secondTenantSession, true, "application/json",
	), http.StatusNotFound)
	crossTenantStatus := fixture.requestWithHeaders(
		http.MethodPost, "/api/v1/reimbursements/"+reimbursementID+"/status-decisions", bytes.NewReader(statusBody), secondTenantSession, true,
		"application/json", map[string]string{"Idempotency-Key": "reimbursement-http-cross-tenant-status"},
	)
	assertStatus(t, crossTenantStatus, http.StatusNotFound)
	for _, privateValue := range []string{tripID, paymentID, assignmentID, reimbursementID, "1234", "合成 HTTP 报销提交"} {
		if strings.Contains(crossTenantStatus.Body.String(), privateValue) {
			t.Fatalf("cross-tenant reimbursement error exposed private value %q: %s", privateValue, crossTenantStatus.Body.String())
		}
	}
}

func activateHTTPTestProvider(t *testing.T, fixture *httpTestFixture, ownerSession *testSession) {
	t.Helper()
	create := fixture.request(
		http.MethodPost,
		"/api/v1/provider-configs",
		strings.NewReader(`{"base_url":"https://provider.example/v1","api_key":"synthetic-test-value","model":"vision-model","output_mode":"json_schema"}`),
		ownerSession,
		true,
		"application/json",
	)
	assertStatus(t, create, http.StatusCreated)
	providerID := asString(t, decodeMap(t, create)["id"])
	assertStatus(t, fixture.request(
		http.MethodPost, "/api/v1/provider-configs/"+providerID+"/detect", nil, ownerSession, true, "",
	), http.StatusOK)
	assertStatus(t, fixture.request(
		http.MethodPost, "/api/v1/provider-configs/"+providerID+"/activate", nil, ownerSession, true, "",
	), http.StatusOK)
}

func processHTTPTestReview(
	t *testing.T,
	fixture *httpTestFixture,
	ownerSession *testSession,
	name string,
	fill color.RGBA,
) map[string]any {
	t.Helper()
	upload := fixture.upload(t, ownerSession, name, "image/png", syntheticPNG(t, fill))
	fixture.processNext(t)
	response := fixture.request(
		http.MethodGet, "/api/v1/reviews/"+asString(t, upload["job_id"]), nil, ownerSession, false, "",
	)
	assertStatus(t, response, http.StatusOK)
	return decodeMap(t, response)
}

func httpConfirmPayload(t *testing.T, review map[string]any, withAssociation bool) []byte {
	t.Helper()
	resolutions := make([]map[string]any, 0)
	if candidates, ok := review["duplicate_candidates"].([]any); ok {
		for _, raw := range candidates {
			candidate, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("HTTP duplicate candidate = %T", raw)
			}
			resolutions = append(resolutions, map[string]any{
				"candidate_id": asString(t, candidate["id"]),
				"action":       domain.DuplicateKeepDistinct,
			})
		}
	}
	payload := map[string]any{
		"expected_revision":     review["revision"],
		"duplicate_resolutions": resolutions,
	}
	if withAssociation {
		mode := "no_candidate"
		if candidates, ok := review["candidates"].([]any); ok && len(candidates) != 0 {
			mode = "reject_all"
		}
		payload["association_mode"] = mode
		payload["allocations"] = []any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func httpTripRevisionPayload(t *testing.T, review map[string]any) []byte {
	t.Helper()
	evidenceID := ""
	fields, ok := review["fields"].([]any)
	if !ok {
		t.Fatalf("HTTP review fields = %T", review["fields"])
	}
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("HTTP review field = %T", raw)
		}
		evidence, _ := field["evidence"].([]any)
		if len(evidence) == 0 {
			continue
		}
		evidenceID = asString(t, evidence[0].(map[string]any)["id"])
		break
	}
	if evidenceID == "" {
		t.Fatal("HTTP review has no reusable synthetic evidence")
	}
	present := func(path, valueType, value string) map[string]any {
		return map[string]any{
			"path": path, "value_type": valueType, "presence": "present",
			"value": value, "evidence_ids": []string{evidenceID},
		}
	}
	absent := func(path, valueType string) map[string]any {
		return map[string]any{"path": path, "value_type": valueType, "presence": "absent"}
	}
	payload := map[string]any{
		"expected_revision":           review["revision"],
		"expected_optimistic_version": review["optimistic_version"],
		"document_type":               domain.DocumentTrip,
		"fields": []map[string]any{
			present("origin", "string", "合成出发地"),
			present("destination", "string", "合成 HTTP 目的地"),
			present("start_date", "date", "2026-08-27"),
			present("end_date", "date", "2026-08-27"),
			absent("traveler_name", "string"),
			absent("transport_type", "string"),
			absent("booking_reference", "string"),
			absent("supplementary_fields", "supplementary"),
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func newHTTPTestFixture(t *testing.T) *httpTestFixture {
	t.Helper()
	root := t.TempDir()
	store := postgresqltest.Open(t)
	var err error
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
	allocationService := allocations.NewService(store, store, system.IDGenerator{}, system.Clock{})
	emailService := applicationemails.NewService(
		store, store, objects, inspector, emailmime.Parser{}, system.IDGenerator{}, system.Clock{},
	)
	tripService := trips.NewService(store, store, system.IDGenerator{}, system.Clock{})
	reimbursementService := reimbursements.NewService(store, store, system.IDGenerator{}, system.Clock{})
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
	insightService := insights.NewService(store)
	server, err := NewServer(authService, uploadService, documentQueries, jobActions, documentDeletions, providerService, reviewService, factService, allocationService, emailService, tripService, reimbursementService, insightService, store, readyFixture{}, logger, Config{Version: "test", WebDistPath: webRoot})
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
	return &httpTestFixture{
		store: store, handler: server.Handler(), worker: worker, owner: owner,
		ownerEmail: ownerEmail, ownerPassword: ownerPassword, emailArchive: emailService,
	}
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
	return f.loginWith(t, f.ownerEmail, f.ownerPassword, tenantID)
}

func (f *httpTestFixture) loginWith(t *testing.T, email, password, tenantID string) *testSession {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"email": email, "password": password, "tenant_id": tenantID})
	if err != nil {
		t.Fatal(err)
	}
	response := f.request(http.MethodPost, "/api/v1/session/login", bytes.NewReader(payload), nil, false, "application/json")
	assertStatus(t, response, http.StatusOK)
	body := decodeMap(t, response)
	return &testSession{cookies: response.Result().Cookies(), csrf: asString(t, body["csrf_token"])}
}

func (f *httpTestFixture) addRoleSession(t *testing.T, role domain.Role) *testSession {
	t.Helper()
	if !role.Valid() || role == domain.RoleOwner {
		t.Fatalf("invalid additional test role %q", role)
	}
	userID := newID(t)
	email := string(role) + "@example.invalid"
	password := string(role) + "-password-123"
	passwordHash, err := (testPasswordHasher{}).Hash([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := f.store.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userID, email, passwordHash, strings.ToUpper(string(role)), now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)
	`, f.owner.TenantID, userID, role, now, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return f.loginWith(t, email, password, f.owner.TenantID)
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

func assertArchiveDownload(t *testing.T, response *httptest.ResponseRecorder, contentType string) {
	t.Helper()
	if response.Header().Get("Content-Type") != contentType ||
		!strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") ||
		response.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox") ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe archive download headers: %#v", response.Header())
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
