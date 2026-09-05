package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestHTTPManualReviewWorkflowPermissionsAndStrictBoundary(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	ctx := context.Background()
	owner := f.login(t, f.owner.TenantID)
	reviewer, viewer := f.addRoleSession(t, domain.RoleReviewer), f.addRoleSession(t, domain.RoleViewer)
	upload := f.upload(t, owner, "manual-source.png", "image/png", syntheticPNG(t, color.RGBA{R: 22, G: 44, B: 66, A: 255}))
	f.processNext(t) // 未配置 Provider，真实 Worker 形成 failed、无 Claim。
	job, err := f.store.GetJob(ctx, f.owner.TenantID, upload["job_id"])
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobFailed {
		t.Fatal("fixture did not fail without provider")
	}
	path := "/api/v1/jobs/" + job.ID + "/manual-review"
	body := fmt.Sprintf(`{"expected_job_version":%d,"document_type":"payment","reason":"按原件人工核对"}`, job.Version)
	headers := map[string]string{"Idempotency-Key": "manual-http-start"}
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), nil, false, "application/json", headers), http.StatusUnauthorized)
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), owner, false, "application/json", headers), http.StatusForbidden)
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), viewer, true, "application/json", headers), http.StatusForbidden)
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(strings.Replace(body, "payment", "unsupported", 1)), reviewer, true, "application/json", headers), http.StatusBadRequest)
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body[:len(body)-1]+`,"unexpected":true}`), reviewer, true, "application/json", headers), http.StatusBadRequest)
	otherTenant := newID(t)
	now := time.Now().UTC()
	if _, err := f.store.DB().Exec(`INSERT INTO tenants (id,name,default_currency,timezone,created_at,updated_at) VALUES (?, '合成其他租户','CNY','Asia/Shanghai',?,?)`, otherTenant, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().Exec(`INSERT INTO memberships (tenant_id,user_id,role,status,created_at,updated_at) VALUES (?,?,'owner','active',?,?)`, otherTenant, f.owner.UserID, now, now); err != nil {
		t.Fatal(err)
	}
	other := f.login(t, otherTenant)
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), other, true, "application/json", headers), http.StatusNotFound)
	created := f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), reviewer, true, "application/json", headers)
	assertStatus(t, created, http.StatusOK)
	replayed := f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), reviewer, true, "application/json", headers)
	assertStatus(t, replayed, http.StatusOK)
	if decodeMap(t, replayed)["replayed"] != true {
		t.Fatal("HTTP replay lost identity")
	}
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(strings.Replace(body, "按原件人工核对", "另一理由", 1)), reviewer, true, "application/json", headers), http.StatusConflict)
	reviewPath := "/api/v1/reviews/" + job.ID
	loaded := f.request(http.MethodGet, reviewPath, nil, reviewer, false, "")
	assertStatus(t, loaded, http.StatusOK)
	review := decodeMap(t, loaded)
	if review["entry_mode"] != "manual" || review["claim_status"] != "blocked" {
		t.Fatal("HTTP source or state incorrect")
	}
	values := map[string]any{"amount_minor": 12345, "currency": "CNY", "merchant": "合成手工商户", "transaction_time": "2026-08-27T12:00:00+08:00", "source_timezone": "Asia/Shanghai"}
	fields := []map[string]any{}
	for _, raw := range review["fields"].([]any) {
		field := raw.(map[string]any)
		name := asString(t, field["path"])
		if name == "document_type" {
			continue
		}
		entry := map[string]any{"path": name, "value_type": field["value_type"], "presence": "absent"}
		if value, ok := values[name]; ok {
			entry["presence"], entry["value"] = "present", value
			entry["manual_evidence"] = []map[string]any{{"page": 1, "quote": "合成原件测试摘录"}}
		}
		fields = append(fields, entry)
	}
	payload, err := json.Marshal(map[string]any{"expected_revision": review["revision"], "expected_optimistic_version": review["optimistic_version"], "document_type": "payment", "fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	revised := f.request(http.MethodPost, reviewPath+"/revisions", bytes.NewReader(payload), reviewer, true, "application/json")
	assertStatus(t, revised, http.StatusCreated)
	review = decodeMap(t, revised)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+job.DocumentID+"/pages/1/content", nil, reviewer, false, ""), http.StatusOK)
	confirmed := f.requestWithHeaders(http.MethodPost, reviewPath+"/confirm", bytes.NewReader(httpConfirmPayload(t, review, true)), reviewer, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-confirm"})
	assertStatus(t, confirmed, http.StatusOK)
	// Reviewer 仅能访问待审核材料；确认后的正式材料继续遵循原有角色边界。
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+job.DocumentID+"/pages/1/content", nil, reviewer, false, ""), http.StatusNotFound)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+job.DocumentID+"/pages/1/content", nil, owner, false, ""), http.StatusOK)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+job.DocumentID+"/pages/1/content", nil, other, false, ""), http.StatusNotFound)
}
