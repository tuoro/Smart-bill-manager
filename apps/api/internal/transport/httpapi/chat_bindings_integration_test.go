package httpapi

import (
	"testing"
)

// 绑定码是这条通道唯一的身份来源，HTTP 层必须守住三件事：需要会话、需要 CSRF、
// 只能看见和解除自己的绑定。
func TestChatBindingEndpointsRequireSessionCSRFAndOwnScope(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	body := map[string]any{"platform": "dingtalk"}

	// 无会话与无 CSRF 都不得放行——这条通道最终能往账目里投单据。
	assertStatus(t, accountRequest(t, f, "POST", "/api/v1/chat-binding-codes", body, nil, true), 401)
	assertStatus(t, accountRequest(t, f, "POST", "/api/v1/chat-binding-codes", body, owner, false), 403)
	assertStatus(t, f.request("GET", "/api/v1/chat-bindings", nil, nil, false, ""), 401)
	assertStatus(t, f.request("DELETE", "/api/v1/chat-bindings/dingtalk", nil, owner, false, ""), 403)

	// 不受支持的平台在入口就拒掉，不落任何记录。
	assertStatus(
		t,
		accountRequest(t, f, "POST", "/api/v1/chat-binding-codes", map[string]any{"platform": "wecom"}, owner, true),
		400,
	)

	issued := decodeMap(t, accountRequest(t, f, "POST", "/api/v1/chat-binding-codes", body, owner, true))
	code := asString(t, issued["code"])
	if code == "" || asString(t, issued["expires_at"]) == "" {
		t.Fatalf("issued = %#v", issued)
	}

	// 还没兑换，绑定列表应当是空的。
	before := decodeMap(t, f.request("GET", "/api/v1/chat-bindings", nil, owner, false, ""))
	if items, ok := before["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("bindings before redeem = %#v", before["items"])
	}

	// 没有绑定时解绑是 404，而不是假装成功。
	assertStatus(t, f.request("DELETE", "/api/v1/chat-bindings/dingtalk", nil, owner, true, ""), 404)
}
