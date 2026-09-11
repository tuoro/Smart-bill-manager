package httpapi

import (
	"bytes"
	"testing"
	"time"
)

// 编辑一组已启用的配置：连接参数原地改、密钥可留空沿用，但检测归零、配置停用、
// 版本递增——改过参数的配置不能带着旧检测结果继续用于识别。
func TestProviderUpdateResetsDetectionKeepsKeyAndIsTenantScoped(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	create := map[string]any{
		"base_url": "https://provider.example/v1", "api_key": "synthetic-key",
		"model": "vision-model", "output_mode": "json_schema",
	}
	created := decodeMap(t, accountRequest(t, f, "POST", "/api/v1/provider-configs", create, owner, true))
	id := asString(t, created["id"])
	path := "/api/v1/provider-configs/" + id
	assertStatus(t, f.request("POST", path+"/detect", nil, owner, true, ""), 200)
	assertStatus(t, f.request("POST", path+"/activate", nil, owner, true, ""), 200)

	edit := map[string]any{
		"base_url": "https://provider.example/v2", "api_key": "",
		"model": "vision-model-2", "output_mode": "json_object",
	}
	assertStatus(t, accountRequest(t, f, "PUT", path, edit, owner, false), 403)
	secondTenantID := newID(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := f.store.DB().Exec("INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES (?, 'Second', 'CNY', 'Asia/Shanghai', ?, ?)", secondTenantID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().Exec("INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES (?, ?, 'owner', 'active', ?, ?)", secondTenantID, f.owner.UserID, now, now); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, accountRequest(t, f, "PUT", path, edit, f.login(t, secondTenantID), true), 404)

	response := accountRequest(t, f, "PUT", path, edit, owner, true)
	assertStatus(t, response, 200)
	updated := decodeMap(t, response)
	if updated["model"] != "vision-model-2" || updated["base_url"] != "https://provider.example/v2" ||
		updated["output_mode"] != "json_object" || updated["capability_status"] != "pending" ||
		updated["active"] != false || updated["version"] != float64(2) ||
		updated["safe_fingerprint"] == created["safe_fingerprint"] {
		t.Fatalf("updated = %#v", updated)
	}
	if _, stale := updated["capability_schema_version"]; stale {
		t.Fatalf("schema identity survived the edit: %#v", updated)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("synthetic-key")) {
		t.Fatal("update echoed the key")
	}
	// 响应是内存里拼的，库里的状态必须单独核对：重新列一次，编辑过的配置
	// 在库里也得是待检测、未启用、版本 2。
	listed := decodeMap(t, f.request("GET", "/api/v1/provider-configs", nil, owner, false, ""))
	items, _ := listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("listed = %#v", listed)
	}
	stored, _ := items[0].(map[string]any)
	if stored["capability_status"] != "pending" || stored["active"] != false ||
		stored["version"] != float64(2) || stored["model"] != "vision-model-2" {
		t.Fatalf("stored after edit = %#v", stored)
	}
	if _, stale := stored["capability_schema_version"]; stale {
		t.Fatalf("stored schema identity survived the edit: %#v", stored)
	}
	// 留空密钥 = 沿用旧密钥：库里密文仍可解开，重新检测照常通过。
	var encrypted []byte
	if err := f.store.DB().QueryRow(`SELECT encrypted_api_key FROM provider_configs WHERE id = ?`, id).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if len(encrypted) == 0 {
		t.Fatal("edit without key dropped the stored key")
	}
	assertStatus(t, f.request("POST", path+"/activate", nil, owner, true, ""), 409)
	detected := decodeMap(t, f.request("POST", path+"/detect", nil, owner, true, ""))
	if detected["capability_status"] != "passed" {
		t.Fatalf("re-detect after edit = %#v", detected)
	}

	// 换密钥：指纹变化，明文仍不落地。
	edit["api_key"] = "rotated-key"
	rotated := decodeMap(t, accountRequest(t, f, "PUT", path, edit, owner, true))
	if rotated["safe_fingerprint"] == updated["safe_fingerprint"] || rotated["version"] != float64(3) {
		t.Fatalf("rotated = %#v", rotated)
	}
	var audits int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM audit_events WHERE action = 'provider_config_updated' AND resource_id = ?`, id).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("audit events = %d", audits)
	}
}
