package httpapi

import (
	"bytes"
	"encoding/json"
	"image/color"
	"net/http"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestHTTPFactCorrectionWorkflowPermissionsAndStrictBoundary(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	finance, reviewer, viewer := f.addRoleSession(t, domain.RoleFinance), f.addRoleSession(t, domain.RoleReviewer), f.addRoleSession(t, domain.RoleViewer)
	activateHTTPTestProvider(t, f, owner)
	review := processHTTPTestReview(t, f, owner, "synthetic-correction.png", color.RGBA{R: 31, G: 99, B: 111, A: 255})
	confirmed := f.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+asString(t, review["job"].(map[string]any)["id"])+"/confirm", bytes.NewReader(httpConfirmPayload(t, review, true)), owner, true, "application/json", map[string]string{"Idempotency-Key": "http-correction-original"})
	assertStatus(t, confirmed, http.StatusOK)
	id := asString(t, decodeMap(t, confirmed)["fact_id"])
	path := "/api/v1/facts/payment/" + id + "/correction"
	assertStatus(t, f.request(http.MethodGet, path, nil, nil, false, ""), http.StatusUnauthorized)
	for _, denied := range []*testSession{reviewer, viewer} {
		assertStatus(t, f.request(http.MethodGet, path, nil, denied, false, ""), http.StatusForbidden)
		assertStatus(t, f.request(http.MethodGet, path+"/history", nil, denied, false, ""), http.StatusForbidden)
	}
	loaded := f.request(http.MethodGet, path, nil, finance, false, "")
	assertStatus(t, loaded, http.StatusOK)
	workspace := decodeMap(t, loaded)
	state := workspace["state"].(map[string]any)
	current := workspace["review"].(map[string]any)
	fields := []map[string]any{}
	for _, raw := range current["fields"].([]any) {
		field := raw.(map[string]any)
		if field["path"] == "document_type" {
			continue
		}
		entry := map[string]any{"path": field["path"], "value_type": field["value_type"], "presence": field["presence"]}
		if value, ok := field["value"]; ok {
			entry["value"] = value
		}
		if field["path"] == "merchant" {
			entry["value"] = "合成 HTTP 更正商户"
			entry["manual_evidence"] = []map[string]any{{"page": 1, "quote": "显式核对合成原件"}}
		}
		fields = append(fields, entry)
	}
	body := map[string]any{"expected_version": state["version"], "current_review_decision_id": state["current_review_decision_id"], "fields": fields, "reason": "合成 HTTP 纠错", "withdraw_link_ids": []string{}}
	encode := func() []byte {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	assertStatus(t, f.request(http.MethodPost, path+"/preview", bytes.NewReader(encode()), finance, false, "application/json"), http.StatusForbidden)
	for _, denied := range []*testSession{reviewer, viewer} {
		assertStatus(t, f.request(http.MethodPost, path+"/preview", bytes.NewReader(encode()), denied, true, "application/json"), http.StatusForbidden)
	}
	body["unexpected"] = true
	assertStatus(t, f.request(http.MethodPost, path+"/preview", bytes.NewReader(encode()), finance, true, "application/json"), http.StatusBadRequest)
	delete(body, "unexpected")
	previewed := f.request(http.MethodPost, path+"/preview", bytes.NewReader(encode()), finance, true, "application/json")
	assertStatus(t, previewed, http.StatusOK)
	preview := decodeMap(t, previewed)
	if preview["can_confirm"] != true {
		t.Fatal("HTTP correction preview blocked")
	}
	body["preview_hash"] = preview["preview_hash"]
	body["acknowledged_duplicate_keys"] = []string{}
	headers := map[string]string{"Idempotency-Key": "http-correction-apply"}
	for _, denied := range []*testSession{reviewer, viewer} {
		assertStatus(t, f.requestWithHeaders(http.MethodPost, path, bytes.NewReader(encode()), denied, true, "application/json", headers), http.StatusForbidden)
	}
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, bytes.NewReader(encode()), finance, false, "application/json", headers), http.StatusForbidden)
	result := f.requestWithHeaders(http.MethodPost, path, bytes.NewReader(encode()), finance, true, "application/json", headers)
	assertStatus(t, result, http.StatusOK)
	if decodeMap(t, result)["fact_id"] != id {
		t.Fatal("HTTP correction replaced fact identity")
	}
	replayed := f.requestWithHeaders(http.MethodPost, path, bytes.NewReader(encode()), finance, true, "application/json", headers)
	assertStatus(t, replayed, http.StatusOK)
	if decodeMap(t, replayed)["replayed"] != true {
		t.Fatal("HTTP correction replay missing")
	}
	assertStatus(t, f.request(http.MethodGet, path+"/history?limit=1", nil, finance, false, ""), http.StatusOK)
	for _, query := range []string{"?limit=0", "?limit=101", "?before_revision=-1", "?limit=1&limit=2", "?other=1"} {
		assertStatus(t, f.request(http.MethodGet, path+"/history"+query, nil, finance, false, ""), http.StatusBadRequest)
	}
	assertStatus(t, f.request(http.MethodGet, strings.Replace(path, id, newID(t), 1), nil, finance, false, ""), http.StatusNotFound)
	assertStatus(t, f.request(http.MethodGet, strings.Replace(path, "/payment/", "/unknown/", 1), nil, finance, false, ""), http.StatusBadRequest)
	latest := f.request(http.MethodGet, path, nil, finance, false, "")
	assertStatus(t, latest, http.StatusOK)
	if decodeMap(t, latest)["review"].(map[string]any)["entry_mode"] != "ai" {
		t.Fatal("HTTP correction forged manual origin")
	}
}
