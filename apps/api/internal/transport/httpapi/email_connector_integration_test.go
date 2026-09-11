package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/mailsync"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

// fakeMailbox 是测试用的 IMAP：密码对不对、有哪些邮件都由测试摆。
type fakeMailbox struct{}

var fakeMailboxState = struct {
	sync.Mutex
	password string
	messages []ports.MailboxMessage
	probes   int
	fetches  int
}{password: "correct-app-password"}

func (fakeMailbox) Probe(_ context.Context, credentials ports.MailboxCredentials) error {
	fakeMailboxState.Lock()
	defer fakeMailboxState.Unlock()
	fakeMailboxState.probes++
	if string(credentials.Password) != fakeMailboxState.password {
		return mailsync.ErrCredentialsRejected
	}
	return nil
}

func (fakeMailbox) FetchNew(_ context.Context, credentials ports.MailboxCredentials, state ports.MailboxState, limit int, handle func(ports.MailboxMessage) error) (ports.MailboxState, error) {
	fakeMailboxState.Lock()
	defer fakeMailboxState.Unlock()
	fakeMailboxState.fetches++
	if string(credentials.Password) != fakeMailboxState.password {
		return state, mailsync.ErrCredentialsRejected
	}
	next := ports.MailboxState{UIDValidity: 7, LastUID: state.LastUID}
	if state.UIDValidity != 7 {
		next.LastUID = 0
	}
	delivered := 0
	for _, message := range fakeMailboxState.messages {
		if message.UID <= next.LastUID || delivered >= limit {
			continue
		}
		if err := handle(message); err != nil {
			return next, err
		}
		next.LastUID = message.UID
		delivered++
	}
	return next, nil
}

type noopMailRuntime struct{}

func (noopMailRuntime) Start(string, string) {}
func (noopMailRuntime) Stop(string, string)  {}

func syntheticRawEmail(t *testing.T, subject string, png []byte) []byte {
	t.Helper()
	return []byte(strings.Join([]string{
		"From: Sender <sender@example.invalid>",
		"To: me@example.invalid",
		"Subject: " + subject,
		"Date: Mon, 01 Sep 2026 10:00:00 +0800",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"b1\"",
		"",
		"--b1",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"see attachment",
		"--b1",
		"Content-Type: image/png; name=\"receipt.png\"",
		"Content-Disposition: attachment; filename=\"receipt.png\"",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString(png),
		"--b1--",
		"",
	}, "\r\n"))
}

// 登记（带密码）→ 检测 → 启用 → 同步：附件进识别队列；密码从不回显；
// 邮箱按人归属；软删除后从列表消失、同一邮箱可重新登记。
func TestEmailSourceConnectorLifecycleAndOwnership(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	memberA := f.addRoleSession(t, domain.RoleMember)
	memberB := f.addRoleSession(t, domain.RoleMember)
	fakeMailboxState.Lock()
	fakeMailboxState.password = "correct-app-password"
	fakeMailboxState.messages = []ports.MailboxMessage{
		{UID: 11, UIDValidity: 7, InternalDate: time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC), Raw: syntheticRawEmail(t, "invoice 1", syntheticPNG(t, color.RGBA{R: 10, G: 20, B: 30, A: 255}))},
		{UID: 12, UIDValidity: 7, InternalDate: time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC), Raw: syntheticRawEmail(t, "invoice 2", syntheticPNG(t, color.RGBA{R: 40, G: 50, B: 60, A: 255}))},
	}
	fakeMailboxState.Unlock()

	register := func(session *testSession, address, password, key string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"display_name":"我的邮箱","mailbox_address":%q,"imap_host":"imap.example.invalid","imap_port":993,"transport_security":"implicit_tls","imap_password":%q}`, address, password)
		return f.requestWithHeaders(http.MethodPost, "/api/v1/email-sources", strings.NewReader(body), session, true, "application/json", map[string]string{"Idempotency-Key": key})
	}
	created := register(memberA, "a@example.invalid", "wrong-password", "member-a-mailbox")
	assertStatus(t, created, http.StatusCreated)
	if bytes.Contains(created.Body.Bytes(), []byte("wrong-password")) {
		t.Fatal("password echoed")
	}
	source := decodeMap(t, created)
	id := asString(t, source["id"])
	if source["has_password"] != true || source["connection_status"] != "pending" || source["sync_enabled"] != false {
		t.Fatalf("created = %#v", source)
	}
	path := "/api/v1/email-sources/" + id

	// 归属：B 看不到 A 的邮箱，也动不了；管理员看得到。
	assertStatus(t, f.request(http.MethodPost, path+"/detect", nil, memberB, true, ""), http.StatusNotFound)
	assertStatus(t, f.request(http.MethodDelete, path, nil, memberB, true, ""), http.StatusNotFound)
	assertStatus(t, f.request(http.MethodGet, path+"/messages", nil, memberB, false, ""), http.StatusNotFound)
	listB := decodeMap(t, f.request(http.MethodGet, "/api/v1/email-sources", nil, memberB, false, ""))
	if items, _ := listB["items"].([]any); len(items) != 0 {
		t.Fatalf("member B saw someone else's mailbox: %#v", listB)
	}
	listOwner := decodeMap(t, f.request(http.MethodGet, "/api/v1/email-sources", nil, owner, false, ""))
	if items, _ := listOwner["items"].([]any); len(items) != 1 {
		t.Fatalf("owner list = %#v", listOwner)
	}

	// 密码错：检测失败，原因可读、不含密码；未通过不能启用。
	failed := decodeMap(t, f.request(http.MethodPost, path+"/detect", nil, memberA, true, ""))
	if failed["connection_status"] != "failed" || !strings.Contains(asString(t, failed["connection_message"]), "拒绝") {
		t.Fatalf("failed detection = %#v", failed)
	}
	assertStatus(t, f.request(http.MethodPost, path+"/activate", nil, memberA, true, ""), http.StatusConflict)

	// 换成正确密码：重置为待检测；检测通过；启用；同步两封。
	credentials := accountRequest(t, f, "PUT", path+"/credentials", map[string]any{"imap_username": "", "imap_password": "correct-app-password"}, memberA, true)
	assertStatus(t, credentials, http.StatusOK)
	if decodeMap(t, credentials)["connection_status"] != "pending" {
		t.Fatalf("credentials reset = %s", credentials.Body.String())
	}
	passed := decodeMap(t, f.request(http.MethodPost, path+"/detect", nil, memberA, true, ""))
	if passed["connection_status"] != "passed" {
		t.Fatalf("passed detection = %#v", passed)
	}
	activated := decodeMap(t, f.request(http.MethodPost, path+"/activate", nil, memberA, true, ""))
	if activated["sync_enabled"] != true {
		t.Fatalf("activated = %#v", activated)
	}
	synced := decodeMap(t, f.request(http.MethodPost, path+"/sync", nil, memberA, true, ""))
	if synced["message_count"] != float64(2) || synced["attachment_count"] != float64(2) || synced["status"] != "active" ||
		!strings.Contains(asString(t, synced["last_sync_message"]), "2") {
		t.Fatalf("synced = %#v", synced)
	}
	var queued int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM documents WHERE ingestion_kind = 'email_attachment'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 2 {
		t.Fatalf("email attachments queued = %d", queued)
	}
	// 再同步一轮：游标已推进，不重复归档。
	again := decodeMap(t, f.request(http.MethodPost, path+"/sync", nil, memberA, true, ""))
	if again["message_count"] != float64(2) || !strings.Contains(asString(t, again["last_sync_message"]), "0") {
		t.Fatalf("second sync = %#v", again)
	}
	// 成员 A 能看自己的邮件，B 不能。
	messages := decodeMap(t, f.request(http.MethodGet, path+"/messages", nil, memberA, false, ""))
	items, _ := messages["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	first, _ := items[0].(map[string]any)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/email-messages/"+asString(t, first["id"])+"/raw", nil, memberB, false, ""), http.StatusNotFound)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/email-messages/"+asString(t, first["id"])+"/raw", nil, memberA, false, ""), http.StatusOK)

	// 删除：列表消失、消息不可再列、同一邮箱可以重新登记；归档的单据留着。
	assertStatus(t, f.request(http.MethodDelete, path, nil, memberA, true, ""), http.StatusNoContent)
	assertStatus(t, f.request(http.MethodGet, path+"/messages", nil, memberA, false, ""), http.StatusNotFound)
	listA := decodeMap(t, f.request(http.MethodGet, "/api/v1/email-sources", nil, memberA, false, ""))
	if items, _ := listA["items"].([]any); len(items) != 0 {
		t.Fatalf("deleted mailbox still listed: %#v", listA)
	}
	var stillQueued int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM documents WHERE ingestion_kind = 'email_attachment'`).Scan(&stillQueued); err != nil {
		t.Fatal(err)
	}
	if stillQueued != 2 {
		t.Fatalf("documents after mailbox deletion = %d", stillQueued)
	}
	assertStatus(t, register(memberA, "a@example.invalid", "correct-app-password", "member-a-mailbox-again"), http.StatusCreated)
}

func TestEmailSourceRegistrationWithoutPasswordStaysPending(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	body := `{"display_name":"无密码","mailbox_address":"np@example.invalid","imap_host":"imap.example.invalid","imap_port":993,"transport_security":"implicit_tls"}`
	created := decodeMap(t, f.requestWithHeaders(http.MethodPost, "/api/v1/email-sources", strings.NewReader(body), owner, true, "application/json", map[string]string{"Idempotency-Key": "no-password"}))
	if created["has_password"] != false {
		t.Fatalf("created = %#v", created)
	}
	path := "/api/v1/email-sources/" + asString(t, created["id"])
	assertStatus(t, f.request(http.MethodPost, path+"/detect", nil, owner, true, ""), http.StatusConflict)
	assertStatus(t, f.request(http.MethodPost, path+"/activate", nil, owner, true, ""), http.StatusConflict)
}
