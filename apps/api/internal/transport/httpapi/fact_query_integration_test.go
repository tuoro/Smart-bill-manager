package httpapi

import (
	"bytes"
	"image/color"
	"net/http"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestHTTPFactManagementQueryDetailsAndSourceBoundary(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	finance, reviewer, viewer := f.addRoleSession(t, domain.RoleFinance), f.addRoleSession(t, domain.RoleReviewer), f.addRoleSession(t, domain.RoleViewer)
	activateHTTPTestProvider(t, f, owner)
	review := processHTTPTestReview(t, f, owner, "synthetic-fact-detail.png", color.RGBA{R: 42, G: 93, B: 111, A: 255})
	confirmed := f.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+asString(t, review["job"].(map[string]any)["id"])+"/confirm", bytes.NewReader(httpConfirmPayload(t, review, true)), owner, true, "application/json", map[string]string{"Idempotency-Key": "http-fact-detail-confirm"})
	assertStatus(t, confirmed, http.StatusOK)
	id := asString(t, decodeMap(t, confirmed)["fact_id"])
	path := "/api/v1/payments/" + id
	for _, session := range []*testSession{owner, finance, viewer} {
		response := f.request(http.MethodGet, path, nil, session, false, "")
		assertStatus(t, response, http.StatusOK)
		body := decodeMap(t, response)
		if body["payment"].(map[string]any)["id"] != id {
			t.Fatal("wrong detail identity")
		}
		source, hasSource := body["source"]
		if session == viewer {
			if hasSource {
				t.Fatal("Viewer received source metadata")
			}
		} else if !hasSource || source.(map[string]any)["claim_set_id"] != review["claim_set_id"] {
			t.Fatal("authorized detail lost source")
		}
		page := f.request(http.MethodGet, "/api/v1/payments?limit=1&allocation_status=unallocated", nil, session, false, "")
		assertStatus(t, page, http.StatusOK)
		if decodeMap(t, page)["next_cursor"] != "" {
			t.Fatal("wrong final cursor")
		}
	}
	assertStatus(t, f.request(http.MethodGet, path, nil, nil, false, ""), http.StatusUnauthorized)
	assertStatus(t, f.request(http.MethodGet, path, nil, reviewer, false, ""), http.StatusForbidden)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/payments", nil, reviewer, false, ""), http.StatusForbidden)
	documentID := asString(t, review["job"].(map[string]any)["document_id"])
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+documentID+"/content", nil, viewer, false, ""), http.StatusForbidden)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+documentID+"/pages/1/content", nil, viewer, false, ""), http.StatusForbidden)
	for _, query := range []string{"limit=0", "limit=101", "limit=x", "limit=1&limit=2", "q=a&q=b", "date_from=2026-02-30", "date_from=2026-09-01&date_to=2026-08-01", "allocation_status=unknown", "cursor=invalid", "unknown=1", "q=%zz"} {
		for _, collection := range []string{"payments", "invoices"} {
			assertStatus(t, f.request(http.MethodGet, "/api/v1/"+collection+"?"+query, nil, owner, false, ""), http.StatusBadRequest)
		}
	}
	assertStatus(t, f.request(http.MethodGet, path+"?include_source=true", nil, viewer, false, ""), http.StatusBadRequest)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/invoices/"+id, nil, owner, false, ""), http.StatusNotFound)
	assertStatus(t, f.request(http.MethodDelete, path, nil, finance, true, ""), http.StatusForbidden)
	assertStatus(t, f.request(http.MethodDelete, path, nil, owner, false, ""), http.StatusForbidden)
	assertStatus(t, f.request(http.MethodDelete, path, nil, owner, true, ""), http.StatusNoContent)
	assertStatus(t, f.request(http.MethodGet, path, nil, owner, false, ""), http.StatusNotFound)
}
