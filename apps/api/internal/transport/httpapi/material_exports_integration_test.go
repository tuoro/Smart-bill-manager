package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func exportHTTPJSON(t *testing.T, f *httpTestFixture, owner *testSession, path string, body any, status int) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response := f.requestWithHeaders(http.MethodPost, path, bytes.NewReader(encoded), owner, true, "application/json", map[string]string{"Idempotency-Key": newID(t)})
	assertStatus(t, response, status)
	return response
}

func correctHTTPExportInvoice(t *testing.T, f *httpTestFixture, owner *testSession, id string) {
	t.Helper()
	path := "/api/v1/facts/invoice/" + id + "/correction"
	loaded := f.request(http.MethodGet, path, nil, owner, false, "")
	assertStatus(t, loaded, http.StatusOK)
	workspace := decodeMap(t, loaded)
	state := workspace["state"].(map[string]any)
	review := workspace["review"].(map[string]any)
	fields := []map[string]any{}
	for _, raw := range review["fields"].([]any) {
		field := raw.(map[string]any)
		if field["path"] == "document_type" {
			continue
		}
		entry := map[string]any{"path": field["path"], "value_type": field["value_type"], "presence": field["presence"]}
		if value, ok := field["value"]; ok {
			entry["value"] = value
		}
		if field["path"] == "seller_name" {
			entry["value"] = "合成交付更正销售方"
			entry["manual_evidence"] = []map[string]any{{"page": 1, "quote": "合成显式原件核对"}}
		}
		fields = append(fields, entry)
	}
	body := map[string]any{"expected_version": state["version"], "current_review_decision_id": state["current_review_decision_id"], "fields": fields, "reason": "合成材料交付前纠错", "withdraw_link_ids": []string{}}
	preview := decodeMap(t, exportHTTPJSON(t, f, owner, path+"/preview", body, http.StatusOK))
	if preview["can_confirm"] != true {
		t.Fatal("synthetic correction blocked")
	}
	body["preview_hash"] = preview["preview_hash"]
	body["acknowledged_duplicate_keys"] = []string{}
	exportHTTPJSON(t, f, owner, path, body, http.StatusOK)
}

func TestHTTPExportManualCorrectionTripMaterialsReimbursementJourney(t *testing.T) {
	f := newHTTPTestFixture(t)
	owner := f.login(t, f.owner.TenantID)
	// 无 Provider 的失败任务，经真实人工审核确认；不是模型成功的替身。
	invoiceID := seedHTTPMaterialInvoice(t, f, owner)
	correctHTTPExportInvoice(t, f, owner, invoiceID)
	created := decodeMap(t, exportHTTPJSON(t, f, owner, "/api/v1/trips", map[string]any{"name": "合成交付行程", "start_date": "2026-08-26", "end_date": "2026-08-28", "timezone": "Asia/Shanghai", "notes": "", "expected_version": 0, "reason": "人工建立材料容器"}, http.StatusCreated))
	tripID := asString(t, created["trip_id"])
	workspace := decodeMap(t, f.request(http.MethodGet, "/api/v1/invoices/"+invoiceID+"/materials", nil, owner, false, ""))
	assignment := decodeMap(t, exportHTTPJSON(t, f, owner, "/api/v1/trip-assignments", map[string]any{"fact_type": "invoice", "fact_id": invoiceID, "desired_trip_id": tripID, "expected_assignment_id": nil, "expected_fact_version": workspace["version"], "reason": "明确纳入合成行程"}, http.StatusOK))
	workspace = decodeMap(t, f.request(http.MethodGet, "/api/v1/invoices/"+invoiceID+"/materials", nil, owner, false, ""))
	buffer, mime := httpMaterialMultipart(t, [][2]string{{"expected_version", fmt.Sprint(workspace["version"])}, {"reason", "合成追加材料"}, {"idempotency_key", "export-http-aux"}}, 1)
	materialResponse := f.request(http.MethodPost, "/api/v1/invoices/"+invoiceID+"/materials/upload", buffer, owner, true, mime)
	assertStatus(t, materialResponse, http.StatusOK)
	material := decodeMap(t, materialResponse)
	selection := []string{asString(t, assignment["assignment_id"])}
	reimbursementPreview := decodeMap(t, exportHTTPJSON(t, f, owner, "/api/v1/reimbursement-previews", map[string]any{"trip_id": tripID, "assignment_ids": selection}, http.StatusOK))
	findingKeys := []string{}
	for _, raw := range reimbursementPreview["findings"].([]any) {
		findingKeys = append(findingKeys, asString(t, raw.(map[string]any)["finding_key"]))
	}
	reimbursement := decodeMap(t, exportHTTPJSON(t, f, owner, "/api/v1/reimbursements", map[string]any{"trip_id": tripID, "assignment_ids": selection, "expected_snapshot_hash": reimbursementPreview["snapshot_hash"], "acknowledged_finding_keys": findingKeys, "reason": "确认合成交付范围"}, http.StatusCreated))
	scope := domain.ExportScope{Kind: "reimbursement", ID: asString(t, reimbursement["reimbursement_id"])}
	previewResponse := exportHTTPJSON(t, f, owner, "/api/v1/material-exports/preview", scope, http.StatusOK)
	var manifest domain.ExportManifest
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 || len(manifest.References) != 2 || !manifest.MaterialsCaptured || len(manifest.Warnings) != 1 {
		t.Fatal("wrong historical package scope")
	}
	for _, reference := range manifest.References {
		if reference.DisplayName != "合成交付更正销售方" || reference.ReviewDecisionID == nil {
			t.Fatal("export lost corrected version")
		}
	}
	prepareBody := map[string]any{"kind": scope.Kind, "id": scope.ID, "expected_manifest_hash": manifest.ManifestHash, "acknowledged_warnings": true}
	prepareBody["acknowledged_warnings"] = false
	exportHTTPJSON(t, f, owner, "/api/v1/material-exports", prepareBody, http.StatusBadRequest)
	prepareBody["acknowledged_warnings"] = true
	prepared := decodeMap(t, exportHTTPJSON(t, f, owner, "/api/v1/material-exports", prepareBody, http.StatusCreated))
	id := asString(t, prepared["id"])
	contentPath := "/api/v1/material-exports/" + id + "/content"
	assertStatus(t, f.request(http.MethodHead, contentPath, nil, owner, false, ""), http.StatusMethodNotAllowed)
	assertStatus(t, f.requestWithHeaders(http.MethodGet, contentPath, nil, owner, false, "", map[string]string{"Range": "bytes=0-10"}), http.StatusBadRequest)
	otherSession := f.login(t, f.owner.TenantID)
	assertStatus(t, f.request(http.MethodGet, contentPath, nil, otherSession, false, ""), http.StatusNotFound)
	download := f.request(http.MethodGet, contentPath, nil, owner, false, "")
	assertStatus(t, download, http.StatusOK)
	if download.Header().Get("Content-Type") != "application/zip" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") || download.Header().Get("Content-Length") != fmt.Sprint(download.Body.Len()) {
		t.Fatal("native download headers invalid")
	}
	archive, err := zip.NewReader(bytes.NewReader(download.Body.Bytes()), int64(download.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 4 {
		t.Fatal("ZIP omitted source or supporting material")
	}
	files := map[string]domain.ExportFile{}
	for _, file := range manifest.Files {
		files[file.Path] = file
	}
	for _, file := range archive.File {
		if expected, ok := files[file.Name]; ok {
			reader, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.New()
			size, err := io.Copy(hash, reader)
			reader.Close()
			if err != nil || size != expected.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
				t.Fatal("ZIP original differs from selected immutable object")
			}
		}
	}
	assertStatus(t, f.request(http.MethodGet, contentPath, nil, owner, false, ""), http.StatusNotFound)
	finance := f.addRoleSession(t, domain.RoleFinance)
	reviewer := f.addRoleSession(t, domain.RoleReviewer)
	viewer := f.addRoleSession(t, domain.RoleViewer)
	exportHTTPJSON(t, f, finance, "/api/v1/material-exports/preview", scope, http.StatusOK)
	for _, denied := range []*testSession{reviewer, viewer} {
		exportHTTPJSON(t, f, denied, "/api/v1/material-exports/preview", scope, http.StatusForbidden)
		exportHTTPJSON(t, f, denied, "/api/v1/material-exports", prepareBody, http.StatusForbidden)
		assertStatus(t, f.request(http.MethodGet, contentPath, nil, denied, false, ""), http.StatusForbidden)
	}
	encoded, _ := json.Marshal(scope)
	assertStatus(t, f.request(http.MethodPost, "/api/v1/material-exports/preview", bytes.NewReader(encoded), owner, false, "application/json"), http.StatusForbidden)
	assertStatus(t, f.request(http.MethodPost, "/api/v1/material-exports/preview", bytes.NewReader(encoded), nil, false, "application/json"), http.StatusUnauthorized)
	for _, body := range []string{`{"kind":"trip","id":"a","id":"b"}`, `{"kind":"trip"}`, `{"kind":"trip","id":null}`, `{"kind":"all","id":"a"}`, `{"kind":"trip","id":"a","extra":1}`, `{"kind":"trip","id":"a"} {}`} {
		assertStatus(t, f.request(http.MethodPost, "/api/v1/material-exports/preview", strings.NewReader(body), owner, true, "application/json"), http.StatusBadRequest)
	}
	exportHTTPJSON(t, f, owner, "/api/v1/material-exports/preview?extra=1", scope, http.StatusBadRequest)
	prepared = decodeMap(t, exportHTTPJSON(t, f, owner, "/api/v1/material-exports", prepareBody, http.StatusCreated))
	cancelPath := "/api/v1/material-exports/" + asString(t, prepared["id"])
	assertStatus(t, f.request(http.MethodDelete, cancelPath, nil, owner, false, ""), http.StatusForbidden)
	assertStatus(t, f.request(http.MethodDelete, cancelPath, nil, owner, true, ""), http.StatusNoContent)
	assertStatus(t, f.request(http.MethodGet, cancelPath+"/content", nil, owner, false, ""), http.StatusNotFound)
	var key string
	if err := f.store.DB().QueryRow(`SELECT storage_key FROM documents WHERE tenant_id=? AND id=?`, f.owner.TenantID, material["document_id"]).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := f.objects.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	failed := exportHTTPJSON(t, f, owner, "/api/v1/material-exports", prepareBody, http.StatusConflict)
	if strings.Contains(failed.Body.String(), key) || !strings.Contains(failed.Body.String(), asString(t, material["document_id"])) {
		t.Fatal("missing-object response leaked path or omitted business identity")
	}
	var providers, manualClaims int
	if err := f.store.DB().QueryRow(`SELECT (SELECT count(*) FROM provider_configs),(SELECT count(*) FROM claim_sets WHERE origin_ai_run_id IS NULL)`).Scan(&providers, &manualClaims); err != nil || providers != 0 || manualClaims < 1 {
		t.Fatal("manual journey invoked provider or fabricated origin")
	}
}
