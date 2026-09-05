package httpapi

import (
	"bytes"
	"fmt"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"image/color"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPBadDebtPermissionsVersionReplayAndTripProtection(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	finance, reviewer, viewer := f.addRoleSession(t, domain.RoleFinance), f.addRoleSession(t, domain.RoleReviewer), f.addRoleSession(t, domain.RoleViewer)
	createBody := `{"name":"合成坏账 HTTP 行程","start_date":"2026-08-27","end_date":"2026-08-27","timezone":"Asia/Shanghai","notes":"","expected_version":0,"reason":"合成测试"}`
	created := f.requestWithHeaders(http.MethodPost, "/api/v1/trips", strings.NewReader(createBody), owner, true, "application/json", map[string]string{"Idempotency-Key": "bad-debt-http-trip"})
	assertStatus(t, created, http.StatusCreated)
	tripID := asString(t, decodeMap(t, created)["trip_id"])
	activateHTTPTestProvider(t, f, owner)
	review := processHTTPTestReview(t, f, owner, "synthetic-bad-debt.png", color.RGBA{R: 31, G: 47, B: 91, A: 255})
	confirmed := f.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+asString(t, review["job"].(map[string]any)["id"])+"/confirm", bytes.NewReader(httpConfirmPayload(t, review, true)), owner, true, "application/json", map[string]string{"Idempotency-Key": "bad-debt-http-confirm"})
	assertStatus(t, confirmed, http.StatusOK)
	id := asString(t, decodeMap(t, confirmed)["fact_id"])
	detail := decodeMap(t, f.request(http.MethodGet, "/api/v1/payments/"+id, nil, owner, false, ""))
	version := int(detail["version"].(float64))
	path := "/api/v1/facts/payment/" + id + "/bad-debt"
	body := fmt.Sprintf(`{"marked":true,"expected_version":%d,"reason":"合成异常标记"}`, version)
	headers := map[string]string{"Idempotency-Key": "bad-debt-http-mark"}
	for _, session := range []*testSession{reviewer, viewer} {
		assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), session, true, "application/json", headers), http.StatusForbidden)
	}
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), finance, false, "application/json", headers), http.StatusForbidden)
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), nil, false, "application/json", headers), http.StatusUnauthorized)
	for _, invalid := range []string{`{"expected_version":1,"reason":"合成"}`, `{"marked":true,"expected_version":1,"reason":""}`, `{"marked":true,"expected_version":0,"reason":"合成"}`} {
		assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(invalid), finance, true, "application/json", headers), http.StatusBadRequest)
	}
	result := f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), finance, true, "application/json", headers)
	assertStatus(t, result, http.StatusOK)
	replay := f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), finance, true, "application/json", headers)
	assertStatus(t, replay, http.StatusOK)
	if decodeMap(t, replay)["replayed"] != true {
		t.Fatal("missing replay")
	}
	detail = decodeMap(t, f.request(http.MethodGet, "/api/v1/payments/"+id, nil, viewer, false, ""))
	if detail["payment"].(map[string]any)["bad_debt"] != true {
		t.Fatal("viewer lost visible bad debt state")
	}
	list := decodeMap(t, f.request(http.MethodGet, "/api/v1/trips", nil, owner, false, ""))
	if list["items"].([]any)[0].(map[string]any)["bad_debt_locked"] != true {
		t.Fatal("trip projection not locked")
	}
	assertStatus(t, f.requestWithHeaders(http.MethodDelete, "/api/v1/trips/"+tripID, strings.NewReader(`{"expected_version":1,"reason":"合成删除"}`), owner, true, "application/json", map[string]string{"Idempotency-Key": "bad-debt-http-delete"}), http.StatusConflict)
	targets := "/api/v1/allocations/payment/" + id + "/targets"
	assertStatus(t, f.request(http.MethodGet, targets+"?view=all_dates&q=合成", nil, finance, false, ""), http.StatusOK)
	assertStatus(t, f.request(http.MethodGet, targets, nil, viewer, false, ""), http.StatusForbidden)
	for _, suffix := range []string{"?q=a&q=b", "?unknown=1", "?view=invalid", "?cursor=invalid"} {
		assertStatus(t, f.request(http.MethodGet, targets+suffix, nil, owner, false, ""), http.StatusBadRequest)
	}
	clearBody := fmt.Sprintf(`{"marked":false,"expected_version":%d,"reason":"已核对取消"}`, version+1)
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(clearBody), finance, true, "application/json", map[string]string{"Idempotency-Key": "bad-debt-http-clear"}), http.StatusOK)
	assertStatus(t, f.requestWithHeaders(http.MethodDelete, "/api/v1/trips/"+tripID, strings.NewReader(`{"expected_version":1,"reason":"合成删除"}`), owner, true, "application/json", map[string]string{"Idempotency-Key": "bad-debt-http-delete"}), http.StatusOK)
}
