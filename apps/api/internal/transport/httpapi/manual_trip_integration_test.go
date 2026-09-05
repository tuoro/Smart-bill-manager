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

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestHTTPManualTripWorkspaceWorkflowAndRoles(t *testing.T) {
	fixture := newHTTPTestFixture(t)
	defer fixture.store.Close()
	ctx := context.Background()
	owner := fixture.login(t, fixture.owner.TenantID)
	finance := fixture.addRoleSession(t, domain.RoleFinance)
	reviewer := fixture.addRoleSession(t, domain.RoleReviewer)
	viewer := fixture.addRoleSession(t, domain.RoleViewer)
	createBody := `{"name":"合成手工行程","start_date":"2026-08-27","end_date":"2026-08-27","timezone":"Asia/Shanghai","notes":"","expected_version":0,"reason":"无需凭证创建"}`
	create := fixture.requestWithHeaders(http.MethodPost, "/api/v1/trips", strings.NewReader(createBody), finance, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-create"})
	assertStatus(t, create, http.StatusCreated)
	tripID := asString(t, decodeMap(t, create)["trip_id"])
	editBody := strings.Replace(strings.Replace(createBody, `"expected_version":0`, `"expected_version":1`, 1), "合成手工行程", "合成已编辑行程", 1)
	assertStatus(t, fixture.requestWithHeaders(http.MethodPatch, "/api/v1/trips/"+tripID, strings.NewReader(editBody), finance, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-edit"}), http.StatusOK)
	var sources int
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM provider_configs) + (SELECT count(*) FROM documents) + (SELECT count(*) FROM ai_runs) + (SELECT count(*) FROM claim_sets) + (SELECT count(*) FROM review_decisions) + (SELECT count(*) FROM fact_field_origins)`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if sources != 0 {
		t.Fatal("manual create/edit fabricated a provider, source, claim or review")
	}
	for index, denied := range []*testSession{reviewer, viewer} {
		assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/trips", strings.NewReader(createBody), denied, true, "application/json", map[string]string{"Idempotency-Key": fmt.Sprintf("manual-http-create-denied-%d", index)}), http.StatusForbidden)
		assertStatus(t, fixture.requestWithHeaders(http.MethodPatch, "/api/v1/trips/"+tripID, strings.NewReader(editBody), denied, true, "application/json", map[string]string{"Idempotency-Key": fmt.Sprintf("manual-http-edit-denied-%d", index)}), http.StatusForbidden)
	}
	for index, body := range []string{strings.Replace(createBody, "Asia/Shanghai", "Invalid/Timezone", 1), strings.Replace(createBody, "2026-08-27", "2026-02-30", 1), strings.Replace(createBody, `"reason":"无需凭证创建"`, `"reason":""`, 1)} {
		assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/trips", strings.NewReader(body), owner, true, "application/json", map[string]string{"Idempotency-Key": fmt.Sprintf("manual-http-invalid-%d", index)}), http.StatusBadRequest)
	}
	deleteBody := `{"expected_version":2,"reason":"删除容器不删除凭证"}`
	assertStatus(t, fixture.requestWithHeaders(http.MethodDelete, "/api/v1/trips/"+tripID, strings.NewReader(deleteBody), finance, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-finance-delete"}), http.StatusForbidden)
	var containers, decisions int
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM trips), (SELECT count(*) FROM trip_management_decisions)`).Scan(&containers, &decisions); err != nil {
		t.Fatal(err)
	}
	if containers != 1 || decisions != 2 {
		t.Fatal("denied or invalid management request wrote state")
	}

	activateHTTPTestProvider(t, fixture, owner)
	paymentReview := processHTTPTestReview(t, fixture, owner, "manual-payment.png", color.RGBA{R: 31, G: 81, B: 131, A: 255})
	confirmed := fixture.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+asString(t, paymentReview["job"].(map[string]any)["id"])+"/confirm",
		bytes.NewReader(httpConfirmPayload(t, paymentReview, true)), reviewer, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-reviewer-payment"})
	assertStatus(t, confirmed, http.StatusOK)
	paymentID := asString(t, decodeMap(t, confirmed)["fact_id"])
	var version int
	var assignedTrip, mode string
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT p.version, p.trip_assignment_mode, a.trip_id FROM payments p JOIN trip_fact_assignments a ON a.tenant_id = p.tenant_id AND a.payment_id = p.id AND a.ended_at IS NULL WHERE p.id = ?`, paymentID).Scan(&version, &mode, &assignedTrip); err != nil {
		t.Fatal(err)
	}
	if assignedTrip != tripID || mode != "auto" {
		t.Fatal("reviewer confirmation did not perform authorized automatic assignment")
	}
	preferenceBody := fmt.Sprintf(`{"mode":"blocked","expected_version":%d}`, version)
	for _, denied := range []*testSession{reviewer, viewer} {
		assertStatus(t, fixture.request(http.MethodPost, "/api/v1/payments/"+paymentID+"/trip-preference", strings.NewReader(preferenceBody), denied, true, "application/json"), http.StatusForbidden)
	}
	assertStatus(t, fixture.request(http.MethodPost, "/api/v1/payments/"+paymentID+"/trip-preference", strings.NewReader(preferenceBody), finance, true, "application/json"), http.StatusNoContent)

	ticket := processHTTPTestReview(t, fixture, owner, "manual-ticket.png", color.RGBA{R: 131, G: 41, B: 91, A: 255})
	jobID := asString(t, ticket["job"].(map[string]any)["id"])
	revised := fixture.request(http.MethodPost, "/api/v1/reviews/"+jobID+"/revisions", bytes.NewReader(httpTripRevisionPayload(t, ticket)), reviewer, true, "application/json")
	assertStatus(t, revised, http.StatusCreated)
	ticket = decodeMap(t, revised)
	ticketConfirmed := fixture.requestWithHeaders(http.MethodPost, "/api/v1/reviews/"+jobID+"/confirm", bytes.NewReader(httpConfirmPayload(t, ticket, false)), reviewer, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-reviewer-ticket"})
	assertStatus(t, ticketConfirmed, http.StatusOK)
	evidenceID := asString(t, decodeMap(t, ticketConfirmed)["fact_id"])
	materialBody, err := json.Marshal(map[string]any{"evidence_id": evidenceID, "desired_trip_id": tripID, "expected_link_id": nil, "expected_version": 1, "reason": "合成材料归集"})
	if err != nil {
		t.Fatal(err)
	}
	for index, denied := range []*testSession{reviewer, viewer} {
		assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/trip-material-assignments", bytes.NewReader(materialBody), denied, true, "application/json", map[string]string{"Idempotency-Key": fmt.Sprintf("manual-http-material-denied-%d", index)}), http.StatusForbidden)
	}
	assertStatus(t, fixture.requestWithHeaders(http.MethodPost, "/api/v1/trip-material-assignments", bytes.NewReader(materialBody), finance, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-material-finance"}), http.StatusOK)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trip-evidence?trip_id="+tripID, nil, viewer, false, ""), http.StatusOK)
	assertStatus(t, fixture.request(http.MethodGet, "/api/v1/trip-evidence", nil, reviewer, false, ""), http.StatusForbidden)
	assertStatus(t, fixture.requestWithHeaders(http.MethodDelete, "/api/v1/trips/"+tripID, strings.NewReader(deleteBody), owner, true, "application/json", map[string]string{"Idempotency-Key": "manual-http-owner-delete"}), http.StatusOK)
	var evidenceAlive bool
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT deleted_at IS NULL FROM trip_evidence_facts WHERE id = ?`, evidenceID).Scan(&evidenceAlive); err != nil {
		t.Fatal(err)
	}
	if !evidenceAlive {
		t.Fatal("container delete removed reviewed evidence")
	}
}
