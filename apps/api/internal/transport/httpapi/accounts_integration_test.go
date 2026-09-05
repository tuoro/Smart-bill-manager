package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func accountRequest(t *testing.T, f *httpTestFixture, method, path string, input any, session *testSession, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return f.request(method, path, bytes.NewReader(payload), session, csrf, "application/json")
}

func TestHTTPMemberInvitationLifecycleAndPassword(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	input := map[string]any{"email": "joined@example.invalid", "role": "reviewer", "reason": "合成邀请", "idempotency_key": "synthetic-http-invite"}
	assertStatus(t, accountRequest(t, f, "POST", "/api/v1/member-invitations", input, owner, false), 403)
	response := accountRequest(t, f, "POST", "/api/v1/member-invitations", input, owner, true)
	assertStatus(t, response, 200)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invitation response is cacheable")
	}
	var created ports.InvitationCreated
	if json.Unmarshal(response.Body.Bytes(), &created) != nil || !domain.ValidInvitationToken(created.Code) {
		t.Fatal("missing single-use invitation")
	}
	replay := accountRequest(t, f, "POST", "/api/v1/member-invitations", input, owner, true)
	assertStatus(t, replay, 200)
	if bytes.Contains(replay.Body.Bytes(), []byte(created.Code)) {
		t.Fatal("replay exposed invitation code")
	}
	list := f.request("GET", "/api/v1/member-invitations", nil, owner, false, "")
	assertStatus(t, list, 200)
	if bytes.Contains(list.Body.Bytes(), []byte(created.Code)) || strings.Contains(list.Body.String(), "token_hash") {
		t.Fatal("invitation list exposed credential")
	}
	detail := f.request("GET", "/api/v1/member-invitations/"+created.Invitation.ID, nil, owner, false, "")
	assertStatus(t, detail, 200)
	if bytes.Contains(detail.Body.Bytes(), []byte(created.Code)) || strings.Contains(detail.Body.String(), "token_hash") {
		t.Fatal("exact invitation exposed credential")
	}
	assertStatus(t, f.request("GET", "/api/v1/member-invitations/missing", nil, owner, false, ""), 404)
	assertStatus(t, f.request("GET", "/api/v1/member-invitations/"+created.Invitation.ID+"?extra=1", nil, owner, false, ""), 400)
	check := accountRequest(t, f, "POST", "/api/v1/invitations/check", map[string]string{"code": created.Code}, nil, false)
	assertStatus(t, check, 200)
	if len(check.Result().Cookies()) != 0 {
		t.Fatal("invitation check created session")
	}
	join := map[string]string{"code": created.Code, "display_name": "合成审核员", "password": "synthetic-http-password"}
	joined := accountRequest(t, f, "POST", "/api/v1/invitations/accept", join, nil, false)
	assertStatus(t, joined, 204)
	if len(joined.Result().Cookies()) != 0 {
		t.Fatal("join created implicit session")
	}
	assertStatus(t, accountRequest(t, f, "POST", "/api/v1/invitations/accept", join, nil, false), 400)
	member := f.loginWith(t, joinEmail(input), join["password"], f.owner.TenantID)
	view := decodeMap(t, f.request("GET", "/api/v1/session", nil, member, false, ""))
	userID := asString(t, view["user"].(map[string]any)["id"])
	assertStatus(t, f.request("GET", "/api/v1/members/"+userID, nil, owner, false, ""), 200)
	assertStatus(t, f.request("GET", "/api/v1/members/"+userID, nil, member, false, ""), 403)
	assertStatus(t, f.request("GET", "/api/v1/members/missing", nil, owner, false, ""), 404)
	assertStatus(t, f.request("GET", "/api/v1/members/"+userID+"?extra=1", nil, owner, false, ""), 400)
	assertStatus(t, f.request("GET", "/api/v1/members", nil, member, false, ""), 403)
	change := map[string]any{"role": "viewer", "status": "suspended", "expected_version": 1, "reason": "合成停用"}
	assertStatus(t, accountRequest(t, f, "PATCH", "/api/v1/members/"+userID, change, owner, true), 200)
	assertStatus(t, f.request("GET", "/api/v1/session", nil, member, false, ""), 401)
	change["status"], change["expected_version"] = "active", 2
	assertStatus(t, accountRequest(t, f, "PATCH", "/api/v1/members/"+userID, change, owner, true), 200)
	assertStatus(t, f.request("GET", "/api/v1/session", nil, member, false, ""), 401)
	member = f.loginWith(t, joinEmail(input), join["password"], f.owner.TenantID)
	password := map[string]string{"current_password": "synthetic-wrong-password", "new_password": "synthetic-http-next-password"}
	wrong := accountRequest(t, f, "POST", "/api/v1/account/password", password, member, true)
	assertStatus(t, wrong, 401)
	if !strings.Contains(wrong.Body.String(), "invalid_credentials") {
		t.Fatal("wrong password confused with expired session")
	}
	assertStatus(t, f.request("GET", "/api/v1/session", nil, member, false, ""), 200)
	password["current_password"] = join["password"]
	assertStatus(t, accountRequest(t, f, "POST", "/api/v1/account/password", password, member, false), 403)
	changed := accountRequest(t, f, "POST", "/api/v1/account/password", password, member, true)
	assertStatus(t, changed, 204)
	for _, cookie := range changed.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			t.Fatal("password change did not clear cookie")
		}
	}
	assertStatus(t, f.request("GET", "/api/v1/session", nil, member, false, ""), 401)
	f.loginWith(t, joinEmail(input), password["new_password"], f.owner.TenantID)
}

func joinEmail(input map[string]any) string { return input["email"].(string) }

func TestHTTPAccountClosedInputsAndFourRoles(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleReviewer, domain.RoleFinance} {
		session := f.addRoleSession(t, role)
		assertStatus(t, f.request("GET", "/api/v1/members", nil, session, false, ""), 403)
		assertStatus(t, f.request("GET", "/api/v1/member-invitations", nil, session, false, ""), 403)
		body := map[string]any{"email": "extra@example.invalid", "role": "viewer", "reason": "合成邀请", "idempotency_key": "synthetic-denied-key"}
		assertStatus(t, accountRequest(t, f, "POST", "/api/v1/member-invitations", body, session, true), 403)
		assertStatus(t, accountRequest(t, f, "PATCH", "/api/v1/members/"+f.owner.UserID, map[string]any{"role": "viewer", "status": "active", "expected_version": 1, "reason": "合成越权"}, session, true), 403)
	}
	assertStatus(t, f.request("GET", "/api/v1/members", nil, owner, false, ""), 200)
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=20&limit=30", "?other=1", "?cursor=invalid"} {
		assertStatus(t, f.request("GET", "/api/v1/members"+query, nil, owner, false, ""), 400)
	}
	for _, body := range []string{`{}`, `{"code":null}`, `{"code":"a","code":"b"}`, `{"Code":"a"}`, `{"code":1}`, `{"code":"a","extra":"b"}`, `{"code":"a"} {}`} {
		assertStatus(t, f.request("POST", "/api/v1/invitations/check", strings.NewReader(body), nil, false, "application/json"), 400)
	}
	assertStatus(t, f.request("POST", "/api/v1/invitations/check?code=synthetic", strings.NewReader(`{"code":"unused"}`), nil, false, "application/json"), 400)
	assertStatus(t, accountRequest(t, f, "POST", "/api/v1/session/workspaces", map[string]string{"email": f.ownerEmail, "password": "synthetic-wrong-password"}, nil, false), 401)
	choices := accountRequest(t, f, "POST", "/api/v1/session/workspaces", map[string]string{"email": f.ownerEmail, "password": f.ownerPassword}, nil, false)
	assertStatus(t, choices, http.StatusOK)
	if len(decodeMap(t, choices)["items"].([]any)) != 1 || len(choices.Result().Cookies()) != 0 {
		t.Fatal("workspace selection changed authentication")
	}
}
