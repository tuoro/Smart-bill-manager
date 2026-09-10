package httpapi

import "testing"

// 凭据端点：要会话、写操作要 CSRF、要集成管理能力；未配置时 404 而不是空对象。
func TestChatConnectorEndpointsGuardSessionCSRFAndCapability(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	body := map[string]any{"app_key": "k", "app_secret": "s"}

	assertStatus(t, f.request("GET", "/api/v1/chat-connectors/dingtalk", nil, nil, false, ""), 401)
	assertStatus(t, accountRequest(t, f, "PUT", "/api/v1/chat-connectors/dingtalk", body, owner, false), 403)
	assertStatus(t, f.request("GET", "/api/v1/chat-connectors/dingtalk", nil, owner, false, ""), 404)
	assertStatus(t, accountRequest(t, f, "PUT", "/api/v1/chat-connectors/wecom", body, owner, true), 400)

	saved := decodeMap(t, accountRequest(t, f, "PUT", "/api/v1/chat-connectors/dingtalk", body, owner, true))
	if saved["app_key"] != "k" || saved["has_secret"] != true || saved["detection_status"] != "pending" || saved["active"] != false {
		t.Fatalf("saved = %#v", saved)
	}
	if _, leaked := saved["app_secret"]; leaked {
		t.Fatal("secret echoed back")
	}
	// 未检测先启用：409。
	assertStatus(t, f.request("POST", "/api/v1/chat-connectors/dingtalk/activate", nil, owner, true, ""), 409)
	detected := decodeMap(t, f.request("POST", "/api/v1/chat-connectors/dingtalk/detect", nil, owner, true, ""))
	if detected["detection_status"] != "passed" {
		t.Fatalf("detected = %#v", detected)
	}
	activated := decodeMap(t, f.request("POST", "/api/v1/chat-connectors/dingtalk/activate", nil, owner, true, ""))
	if activated["active"] != true {
		t.Fatalf("activated = %#v", activated)
	}
	deactivated := decodeMap(t, f.request("POST", "/api/v1/chat-connectors/dingtalk/deactivate", nil, owner, true, ""))
	if deactivated["active"] != false {
		t.Fatalf("deactivated = %#v", deactivated)
	}
}
