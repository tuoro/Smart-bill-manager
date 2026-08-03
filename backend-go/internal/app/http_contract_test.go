//go:build cgo

package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/models"
)

type contractResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Error   string               `json:"error"`
	Data    json.RawMessage      `json:"data"`
	User    *models.UserResponse `json:"user"`
	Token   string               `json:"token"`
}

type contractUser struct {
	ID       string
	Username string
	Token    string
}

func TestAuthenticationHTTPContracts(t *testing.T) {
	application := newTestApplication(t)

	initial := performContractRequest(t, application, http.MethodGet, "/api/auth/setup-required", "", nil, nil)
	assertContractStatus(t, initial, http.StatusOK)
	var setupState struct {
		SetupRequired bool `json:"setupRequired"`
	}
	decodeContractData(t, initial, &setupState)
	if !setupState.SetupRequired {
		t.Fatal("空数据库应要求完成初始化")
	}

	publicRegister := performContractRequest(t, application, http.MethodPost, "/api/auth/register", "", map[string]any{
		"username": "member",
		"password": "secret12",
	}, nil)
	assertContractStatus(t, publicRegister, http.StatusForbidden)
	if !strings.Contains(publicRegister.body.Message, "Setup") {
		t.Fatalf("未初始化时应引导 Setup，实际响应为 %#v", publicRegister.body)
	}

	admin := setupContractAdmin(t, application)
	if admin.Username != "admin" || admin.Token == "" {
		t.Fatalf("初始化管理员响应异常: %#v", admin)
	}

	repeatedSetup := performContractRequest(t, application, http.MethodPost, "/api/auth/setup", "", map[string]any{
		"username": "other-admin",
		"password": "secret12",
	}, nil)
	assertContractStatus(t, repeatedSetup, http.StatusForbidden)

	wrongLogin := performContractRequest(t, application, http.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "admin",
		"password": "wrong-password",
	}, nil)
	assertContractStatus(t, wrongLogin, http.StatusUnauthorized)
	if wrongLogin.body.Error != "" {
		t.Fatalf("认证失败不应返回内部错误: %#v", wrongLogin.body)
	}

	login := performContractRequest(t, application, http.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "admin",
		"password": "secret12",
	}, nil)
	assertContractStatus(t, login, http.StatusOK)
	if login.body.Token == "" || login.body.User == nil || login.body.User.ID != admin.ID {
		t.Fatalf("登录响应异常: %#v", login.body)
	}
	if strings.Contains(login.raw, "password") {
		t.Fatalf("登录响应不应包含密码字段: %s", login.raw)
	}

	missingToken := performContractRequest(t, application, http.MethodGet, "/api/auth/me", "", nil, nil)
	assertContractStatus(t, missingToken, http.StatusUnauthorized)

	me := performContractRequest(t, application, http.MethodGet, "/api/auth/me", login.body.Token, nil, nil)
	assertContractStatus(t, me, http.StatusOK)
	var currentUser models.UserResponse
	decodeContractData(t, me, &currentUser)
	if currentUser.ID != admin.ID || currentUser.Role != "admin" {
		t.Fatalf("当前用户响应异常: %#v", currentUser)
	}
}

func TestAuthorizationAndActAsHTTPContracts(t *testing.T) {
	application := newTestApplication(t)
	admin := setupContractAdmin(t, application)
	member := registerContractMember(t, application, admin.Token)

	memberAdminList := performContractRequest(t, application, http.MethodGet, "/api/admin/users", member.Token, nil, nil)
	assertContractStatus(t, memberAdminList, http.StatusForbidden)

	unknownTarget := performContractRequest(t, application, http.MethodGet, "/api/payments", admin.Token, nil, map[string]string{
		middleware.HeaderActAsUser: "missing-user",
	})
	assertContractStatus(t, unknownTarget, http.StatusBadRequest)

	paymentInput := map[string]any{
		"amount":           12.34,
		"merchant":         "契约测试商户",
		"transaction_time": "2026-08-03T08:00:00+08:00",
	}
	confirmationRequired := performContractRequest(t, application, http.MethodPost, "/api/payments", admin.Token, paymentInput, map[string]string{
		middleware.HeaderActAsUser: member.ID,
	})
	assertContractStatus(t, confirmationRequired, http.StatusBadRequest)
	var confirmation struct {
		Code         string `json:"code"`
		ActorUserID  string `json:"actor_user_id"`
		TargetUserID string `json:"target_user_id"`
		Method       string `json:"method"`
		Path         string `json:"path"`
	}
	decodeContractData(t, confirmationRequired, &confirmation)
	if confirmation.Code != "ACT_AS_CONFIRM_REQUIRED" || confirmation.ActorUserID != admin.ID ||
		confirmation.TargetUserID != member.ID || confirmation.Method != http.MethodPost || confirmation.Path != "/api/payments" {
		t.Fatalf("代操作确认响应异常: %#v", confirmation)
	}
	assertPaymentCount(t, application, 0)

	confirmed := performContractRequest(t, application, http.MethodPost, "/api/payments", admin.Token, paymentInput, map[string]string{
		middleware.HeaderActAsUser:      member.ID,
		middleware.HeaderActAsConfirmed: "1",
	})
	assertContractStatus(t, confirmed, http.StatusCreated)
	var actedPayment models.Payment
	decodeContractData(t, confirmed, &actedPayment)
	if actedPayment.OwnerUserID != member.ID || actedPayment.Amount != 12.34 {
		t.Fatalf("代操作创建的支付归属异常: %#v", actedPayment)
	}

	memberPayment := performContractRequest(t, application, http.MethodPost, "/api/payments", member.Token, map[string]any{
		"amount":           8.88,
		"merchant":         "普通用户商户",
		"transaction_time": "2026-08-03T09:00:00+08:00",
	}, map[string]string{
		middleware.HeaderActAsUser: admin.ID,
	})
	assertContractStatus(t, memberPayment, http.StatusCreated)
	var ownPayment models.Payment
	decodeContractData(t, memberPayment, &ownPayment)
	if ownPayment.OwnerUserID != member.ID {
		t.Fatalf("普通用户不应通过代操作头切换归属: %#v", ownPayment)
	}

	adminReadAsMember := performContractRequest(t, application, http.MethodGet, "/api/payments", admin.Token, nil, map[string]string{
		middleware.HeaderActAsUser: member.ID,
	})
	assertContractStatus(t, adminReadAsMember, http.StatusOK)
	var list struct {
		Items []models.Payment `json:"items"`
		Total int64            `json:"total"`
	}
	decodeContractData(t, adminReadAsMember, &list)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("管理员代读应只看到目标用户数据: %#v", list)
	}

	disableMember := performContractRequest(t, application, http.MethodPatch, "/api/admin/users/"+member.ID+"/active", admin.Token, map[string]any{
		"is_active": false,
	}, nil)
	assertContractStatus(t, disableMember, http.StatusOK)

	disabledToken := performContractRequest(t, application, http.MethodGet, "/api/auth/me", member.Token, nil, nil)
	assertContractStatus(t, disabledToken, http.StatusUnauthorized)
	if disabledToken.body.Message != "账号已停用" {
		t.Fatalf("停用账号的旧令牌应立即失效: %#v", disabledToken.body)
	}
}

type recordedContractResponse struct {
	status int
	raw    string
	body   contractResponse
}

func setupContractAdmin(t *testing.T, application *Application) contractUser {
	t.Helper()
	response := performContractRequest(t, application, http.MethodPost, "/api/auth/setup", "", map[string]any{
		"username": "admin",
		"password": "secret12",
	}, nil)
	assertContractStatus(t, response, http.StatusCreated)
	if response.body.User == nil {
		t.Fatalf("初始化响应缺少用户: %s", response.raw)
	}
	return contractUser{ID: response.body.User.ID, Username: response.body.User.Username, Token: response.body.Token}
}

func registerContractMember(t *testing.T, application *Application, adminToken string) contractUser {
	t.Helper()
	invite := performContractRequest(t, application, http.MethodPost, "/api/admin/invites", adminToken, map[string]any{
		"expiresInDays": 7,
	}, nil)
	assertContractStatus(t, invite, http.StatusOK)
	var inviteData struct {
		Code string `json:"code"`
	}
	decodeContractData(t, invite, &inviteData)
	if inviteData.Code == "" {
		t.Fatalf("创建邀请码响应缺少明文邀请码: %s", invite.raw)
	}

	registered := performContractRequest(t, application, http.MethodPost, "/api/auth/invite/register", "", map[string]any{
		"inviteCode": inviteData.Code,
		"username":   "member",
		"password":   "secret12",
	}, nil)
	assertContractStatus(t, registered, http.StatusCreated)
	if registered.body.User == nil {
		t.Fatalf("邀请注册响应缺少用户: %s", registered.raw)
	}
	return contractUser{ID: registered.body.User.ID, Username: registered.body.User.Username, Token: registered.body.Token}
}

func performContractRequest(
	t *testing.T,
	application *Application,
	method string,
	path string,
	token string,
	body any,
	headers map[string]string,
) recordedContractResponse {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("编码请求体失败: %v", err)
		}
	}
	request := httptest.NewRequest(method, path, &payload)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	application.Router.ServeHTTP(recorder, request)
	raw := recorder.Body.String()
	var decoded contractResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("解析 %s %s 响应失败: status=%d body=%q err=%v", method, path, recorder.Code, raw, err)
	}
	return recordedContractResponse{status: recorder.Code, raw: raw, body: decoded}
}

func assertContractStatus(t *testing.T, response recordedContractResponse, want int) {
	t.Helper()
	if response.status != want {
		t.Fatalf("状态码应为 %d，实际为 %d，响应为 %s", want, response.status, response.raw)
	}
}

func decodeContractData(t *testing.T, response recordedContractResponse, target any) {
	t.Helper()
	if len(response.body.Data) == 0 || string(response.body.Data) == "null" {
		t.Fatalf("响应缺少 data: %s", response.raw)
	}
	if err := json.Unmarshal(response.body.Data, target); err != nil {
		t.Fatalf("解析 data 失败: body=%s err=%v", response.raw, err)
	}
}

func assertPaymentCount(t *testing.T, application *Application, want int64) {
	t.Helper()
	var count int64
	if err := application.db.Model(&models.Payment{}).Count(&count).Error; err != nil {
		t.Fatalf("统计支付记录失败: %v", err)
	}
	if count != want {
		t.Fatalf("支付记录数应为 %d，实际为 %d", want, count)
	}
}
