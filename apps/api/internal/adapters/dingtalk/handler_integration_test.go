package dingtalk

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatdialogue"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatintake"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

var pixelPNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
)

// 假下载器：按 downloadCode 返回预置内容；假回复器：记录回复文案。
type fakeFiles struct {
	blobs map[string][]byte
	err   error
}

func (f fakeFiles) DownloadMessageFile(_ context.Context, code string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	blob, ok := f.blobs[code]
	if !ok {
		return nil, errors.New("unknown download code")
	}
	return blob, nil
}

type fakeReplier struct {
	replies  []string
	webhooks []string
}

func (r *fakeReplier) SimpleReplyText(_ context.Context, webhook string, content []byte) error {
	r.replies = append(r.replies, string(content))
	r.webhooks = append(r.webhooks, webhook)
	return nil
}

type fixture struct {
	handler  *Handler
	replier  *fakeReplier
	intake   chatintake.Service
	store    *postgresqladapter.Store
	owner    ports.BootstrapOwner
	notifier *fakeNotifier
}

type fakeNotifier struct {
	sent []string
}

func (n *fakeNotifier) Send(_ context.Context, _, _, _, text string) error {
	n.sent = append(n.sent, text)
	return nil
}

func newFixture(t *testing.T, files Downloader) fixture {
	t.Helper()
	ctx := context.Background()
	store := postgresqltest.Open(t)
	owner := ports.BootstrapOwner{
		UserID: "00000000-0000-4000-8000-000000000101", TenantID: "00000000-0000-4000-8000-000000000102",
		Email: "owner@example.test", PasswordHash: "test-only", DisplayName: "Owner", TenantName: "Tenant",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	clock := fixedClock{now: time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)}
	uploads := documents.NewUploadService(objects, inspector, store, system.IDGenerator{}, clock)
	intake := chatintake.NewService(store, uploads, cryptography.TokenGenerator{}, system.IDGenerator{}, clock)
	replier := &fakeReplier{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// 文本走真实的对话服务：绑定码的判断只有那一处实现，这里不摆一个替身。
	notifier := &fakeNotifier{}
	dialogue := chatdialogue.NewService(store, store,
		reviews.NewService(store, store, system.IDGenerator{}, clock), intake, notifier, clock, logger)
	return fixture{
		handler: NewHandler(owner.TenantID, intake, dialogue, files, replier, logger),
		replier: replier, intake: intake, store: store, owner: owner, notifier: notifier,
	}
}

func (f fixture) issueCode(t *testing.T) string {
	t.Helper()
	issued, err := f.intake.IssueBindingCode(context.Background(), domain.TenantContext{
		TenantID: f.owner.TenantID, UserID: f.owner.UserID, Role: domain.RoleOwner,
	}, domain.ChatPlatformDingTalk)
	if err != nil {
		t.Fatal(err)
	}
	return issued.Code
}

func (f fixture) documents(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM documents`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func message(msgtype, staffID string, content map[string]any, text string) *chatbot.BotCallbackDataModel {
	return &chatbot.BotCallbackDataModel{
		MsgId: "msg-1", Msgtype: msgtype, SenderStaffId: staffID, SessionWebhook: "https://hook.example/s1",
		Text: chatbot.BotCallbackDataTextModel{Content: text}, Content: content,
	}
}

func (f fixture) handle(t *testing.T, data *chatbot.BotCallbackDataModel) string {
	t.Helper()
	if _, err := f.handler.Handle(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if len(f.replier.replies) == 0 {
		t.Fatal("no reply sent")
	}
	return f.replier.replies[len(f.replier.replies)-1]
}

// 发一条文本 = 兑换绑定码；发文件 = 投递。全程用 senderStaffId 认人，用回调里的
// sessionWebhook 回话。这是连接器唯一的职责：翻译，不做第二套判断。
func TestHandlerBindsThenAcceptsFilesAndReplies(t *testing.T) {
	f := newFixture(t, fakeFiles{blobs: map[string][]byte{"dl-png": pixelPNG}})

	// 未绑定先发文件：拒收且不落任何文档。
	if got := f.handle(t, message("file", "staff-1", map[string]any{"downloadCode": "dl-png", "fileName": "pay.png"}, "")); got != replyNotLinked {
		t.Fatalf("reply = %q", got)
	}
	if f.documents(t) != 0 {
		t.Fatal("unlinked sender created a document")
	}

	// 乱猜的码。
	if got := f.handle(t, message("text", "staff-1", nil, "not-a-code")); got != replyInvalidCode {
		t.Fatalf("reply = %q", got)
	}
	// 真码。
	if got := f.handle(t, message("text", "staff-1", nil, "  "+f.issueCode(t)+"  ")); got != replyBound {
		t.Fatalf("reply = %q", got)
	}
	// 绑定后投件成功，来源标记为钉钉。
	if got := f.handle(t, message("file", "staff-1", map[string]any{"downloadCode": "dl-png", "fileName": "pay.png"}, "")); got != "已收到 pay.png，正在识别。" {
		t.Fatalf("reply = %q", got)
	}
	var kind string
	if err := f.store.DB().QueryRow(`SELECT ingestion_kind FROM documents`).Scan(&kind); err != nil || kind != domain.DocumentIngestionDingTalk {
		t.Fatalf("ingestion kind = %q, err = %v", kind, err)
	}
	// 重投同一份：报已收过，不再建。
	if got := f.handle(t, message("picture", "staff-1", map[string]any{"downloadCode": "dl-png"}, "")); got != replyDuplicate {
		t.Fatalf("reply = %q", got)
	}
	if f.documents(t) != 1 {
		t.Fatalf("documents = %d", f.documents(t))
	}
	if f.replier.webhooks[0] != "https://hook.example/s1" {
		t.Fatalf("webhook = %q", f.replier.webhooks[0])
	}
}

// 不认文件名后缀，只看字节：钉钉给的名字可以随便改，库里 declared_mime 必须等于探测值。
func TestHandlerRejectsContentThatIsNotImageOrPDF(t *testing.T) {
	f := newFixture(t, fakeFiles{blobs: map[string][]byte{"dl-txt": []byte("hello, not an image")}})
	f.handle(t, message("text", "staff-2", nil, f.issueCode(t)))
	if got := f.handle(t, message("file", "staff-2", map[string]any{"downloadCode": "dl-txt", "fileName": "fake.png"}, "")); got != replyBadContent {
		t.Fatalf("reply = %q", got)
	}
	if f.documents(t) != 0 {
		t.Fatal("non-image content created a document")
	}
}

func TestHandlerExplainsUnsupportedMessagesAndFailures(t *testing.T) {
	f := newFixture(t, fakeFiles{err: ErrFileTooLarge})
	f.handle(t, message("text", "staff-3", nil, f.issueCode(t)))
	if got := f.handle(t, message("audio", "staff-3", nil, "")); got != replyUnsupportedMsg {
		t.Fatalf("audio reply = %q", got)
	}
	// 已绑定、手头没有待确认的单据：告诉他现在没事可做，而不是"绑定码无效"。
	if got := f.handle(t, message("text", "staff-3", nil, "   ")); !strings.HasPrefix(got, "现在没有待确认的单据。") {
		t.Fatalf("blank text reply = %q", got)
	}
	if got := f.handle(t, message("file", "staff-3", map[string]any{"fileName": "x.pdf"}, "")); got != replyDownloadFailed {
		t.Fatalf("missing code reply = %q", got)
	}
	if got := f.handle(t, message("file", "staff-3", map[string]any{"downloadCode": "any"}, "")); got != replyTooLarge {
		t.Fatalf("too large reply = %q", got)
	}
}

// 图片消息钉钉不给文件名，名字由我们合成。MsgId 是 base64 串，含 "/" 时直接拿来当
// 文件名会被上传路径的 filepath.Base 截断，收件箱里只剩一段无意义残片。
func TestSynthesizedPictureNameSurvivesUploadNormalization(t *testing.T) {
	f := newFixture(t, fakeFiles{blobs: map[string][]byte{"dl-png": pixelPNG}})
	if got := f.handle(t, message("text", "staff-1", nil, f.issueCode(t))); got != replyBound {
		t.Fatalf("bind = %q", got)
	}
	picture := message("picture", "staff-1", map[string]any{"downloadCode": "dl-png"}, "")
	picture.MsgId = "msg7Iz4mIIv1kB5L0SymB/H6w=="
	reply := f.handle(t, picture)

	var stored string
	if err := f.store.DB().QueryRow(`SELECT original_name FROM documents WHERE ingestion_kind = 'dingtalk_message'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "钉钉图片-") || !strings.HasSuffix(stored, ".png") || strings.ContainsAny(stored, "/+=") {
		t.Fatalf("stored document name = %q", stored)
	}
	if reply != "已收到 "+stored+"，正在识别。" {
		t.Fatalf("reply = %q, stored = %q", reply, stored)
	}
	// 同一条消息重投得到同一个名字：名字来自 MsgId 的摘要，不是随机数。
	if again := synthesizedName("钉钉图片", picture.MsgId); !strings.HasPrefix(stored, again) {
		t.Fatalf("name is not deterministic: %q vs %q", stored, again)
	}
}
