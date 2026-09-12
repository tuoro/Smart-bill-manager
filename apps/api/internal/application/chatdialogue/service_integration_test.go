package chatdialogue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/chatintake"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/insights"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/processing"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

type movingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movingClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type recordingNotifier struct {
	mu   sync.Mutex
	sent []string
}

func (n *recordingNotifier) Send(_ context.Context, _, _, _, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, text)
	return nil
}

func (n *recordingNotifier) last() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.sent) == 0 {
		return ""
	}
	return n.sent[len(n.sent)-1]
}

// 合成抽取器：不联网，交出测试当前编排的那份识别结果。
type scriptedExtractor struct {
	next *domain.BillVisibleTextEnvelope
}

func (scriptedExtractor) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/2", SHA256: strings.Repeat("c", 64)}
}

func (e scriptedExtractor) Prepare(_ ports.ProviderCredentials, pages []ports.PageImage) (ports.PreparedBillExtraction, error) {
	if len(pages) != 1 {
		return nil, errors.New("expected one normalized page")
	}
	return scriptedPrepared{envelope: *e.next}, nil
}

type scriptedPrepared struct {
	envelope domain.BillVisibleTextEnvelope
}

func (scriptedPrepared) RequestHash() string { return strings.Repeat("a", 64) }

func (scriptedPrepared) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/2", SHA256: strings.Repeat("c", 64)}
}

func (p scriptedPrepared) Execute(context.Context) (ports.BillExtractionResult, error) {
	return ports.BillExtractionResult{Envelope: p.envelope, ResponseHash: strings.Repeat("d", 64)}, nil
}

func paymentEnvelope(amount, merchant string) domain.BillVisibleTextEnvelope {
	return domain.BillVisibleTextEnvelope{
		SchemaVersion: "bill-visible-text/2",
		DocumentType:  string(domain.DocumentPayment),
		Payment: json.RawMessage(`{"amount":{"text":"CNY ` + amount + `","page":1},"currency":{"text":"CNY","page":1},` +
			`"merchant":{"text":"` + merchant + `","page":1},"transaction_time":{"text":"2026-09-12 20:29","page":1},` +
			`"timezone":{"text":"Asia/Shanghai","page":1},"payment_method":null,"order_number":null,"category":null}`),
		Invoice: json.RawMessage(`null`),
	}
}

func invoiceEnvelope(amount, seller, number string) domain.BillVisibleTextEnvelope {
	return domain.BillVisibleTextEnvelope{
		SchemaVersion: "bill-visible-text/2",
		DocumentType:  string(domain.DocumentInvoice),
		Payment:       json.RawMessage(`null`),
		Invoice: json.RawMessage(`{"invoice_number":{"text":"` + number + `","page":1},` +
			`"invoice_date":{"text":"2026-09-12","page":1},"amount_without_tax":null,"tax_amount":null,` +
			`"amount_with_tax":{"text":"` + amount + `","page":1},"currency":{"text":"CNY","page":1},` +
			`"seller_name":{"text":"` + seller + `","page":1},"buyer_name":{"text":"合成买方","page":1},"items":[]}`),
	}
}

type fixture struct {
	store    *postgresqladapter.Store
	intake   chatintake.Service
	dialogue Service
	worker   *processing.Worker
	notifier *recordingNotifier
	clock    *movingClock
	tenant   domain.TenantContext
	staffID  string
	envelope *domain.BillVisibleTextEnvelope
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	store := postgresqltest.Open(t)
	ids := system.IDGenerator{}
	clock := &movingClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	owner := ports.BootstrapOwner{
		UserID: "00000000-0000-4000-8000-000000000101", TenantID: "00000000-0000-4000-8000-000000000102",
		Email: "owner@example.test", PasswordHash: "test-only", DisplayName: "Owner", TenantName: "Tenant",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "Asia/Shanghai", CreatedAt: clock.Now(),
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	cipher, err := cryptography.NewSecretCipher(bytes.Repeat([]byte{0x43}, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt([]byte("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	identity := ports.ProviderSchemaIdentity{Version: "bill-visible-text-provider/2", SHA256: strings.Repeat("c", 64)}
	providerID := "00000000-0000-4000-8000-000000000103"
	if err := store.WithinTransaction(ctx, func(tx ports.Transaction) error {
		if err := tx.InsertProviderConfig(ctx, ports.ProviderConfig{
			ID: providerID, TenantID: owner.TenantID, BaseURL: "https://provider.example/v1",
			EncryptedAPIKey: encrypted, Model: "test-model", OutputMode: ports.ProviderOutputModeJSONSchema,
			CapabilityStatus: "passed", CapabilitySafeMessage: "passed",
			CapabilitySchemaVersion: identity.Version, CapabilitySchemaSHA256: identity.SHA256,
			Version: 1, SafeFingerprint: "test-fingerprint", CreatedByUserID: owner.UserID,
			CreatedAt: clock.Now(), UpdatedAt: clock.Now(),
		}); err != nil {
			return err
		}
		return tx.ActivateProviderConfig(ctx, owner.TenantID, providerID, 1, identity, clock.Now())
	}); err != nil {
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
	normalizer, err := localstorage.NewNormalizer(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	uploads := documents.NewUploadService(objects, inspector, store, ids, clock)
	intake := chatintake.NewService(store, uploads, cryptography.TokenGenerator{}, ids, clock)
	envelope := paymentEnvelope("61.46", "美团")
	worker, err := processing.NewWorker(store, store, cipher, normalizer, objects, scriptedExtractor{next: &envelope},
		store, ids, clock, logger, processing.WorkerConfig{
			Concurrency: 1, JobTimeout: 150 * time.Second, LeaseDuration: 165 * time.Second,
			PollInterval: 100 * time.Millisecond,
		})
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	deletions := documents.NewDeletionService(store, objects, store, ids, clock)
	reviewService := reviews.NewService(store, store, ids, clock).WithDiscard(deletions)
	dialogue := NewService(store, store, reviewService, intake, notifier, clock, logger).
		WithQueries(insights.NewService(store), documents.NewQueryService(store, store, objects))
	worker.OnJobFinished(dialogue.Announce)

	tenant := domain.TenantContext{TenantID: owner.TenantID, UserID: owner.UserID, Role: domain.RoleOwner}
	issued, err := intake.IssueBindingCode(ctx, tenant, domain.ChatPlatformDingTalk)
	if err != nil {
		t.Fatal(err)
	}
	staffID := "staff-1"
	if _, err := intake.RedeemBindingCode(ctx, domain.ChatPlatformDingTalk, staffID, issued.Code, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	return fixture{store: store, intake: intake, dialogue: dialogue, worker: worker,
		notifier: notifier, clock: clock, tenant: tenant, staffID: staffID, envelope: &envelope}
}

// deliver 走完整条路：钉钉投件 → 真实 Worker 识别 → 回执推送。
// seed 决定图案，variant 只动一个像素——同 seed 不同 variant 就是"视觉近似但字节
// 不同"，正是重复检测要认的那种。
func (f fixture) deliver(t *testing.T, name string, seed, variant int) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.intake.Receive(ctx, chatintake.Message{
		Platform: domain.ChatPlatformDingTalk, ExternalUserID: f.staffID,
		FileName: name, MIME: "image/png", Source: bytes.NewReader(patternPNG(t, seed, variant)),
		TenantID: f.tenant.TenantID,
	}); err != nil {
		t.Fatal(err)
	}
	now := f.clock.Now()
	job, err := f.store.LeaseNextJob(ctx, "chat-test", now, now.Add(165*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.worker.ProcessOne(ctx, job); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) session(t *testing.T) (ports.ChatSession, bool) {
	t.Helper()
	session, err := f.store.FindChatSession(context.Background(), domain.ChatPlatformDingTalk, f.staffID)
	if errors.Is(err, domain.ErrNotFound) {
		return ports.ChatSession{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return session, true
}

func (f fixture) facts(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM payments WHERE deleted_at IS NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func patternPNG(t *testing.T, seed, variant int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			level := uint8((x*7 + y*seed*13) % 200)
			canvas.SetRGBA(x, y, color.RGBA{R: level, G: level, B: level, A: 255})
		}
	}
	if variant > 0 {
		// 只改一个像素的蓝色分量：字节变了，亮度几乎没动，视觉指纹仍然判为近似。
		canvas.SetRGBA(0, 0, color.RGBA{R: 0, G: 0, B: uint8(variant), A: 255})
	}
	var content bytes.Buffer
	if err := png.Encode(&content, canvas); err != nil {
		t.Fatal(err)
	}
	return content.Bytes()
}

// 识别完推回执，回「确认」即入账——与网页上按确认走的是同一条用例。
func TestReceiptThenConfirmCreatesFactAndClosesSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.deliver(t, "meituan.png", 1, 0)

	card := f.notifier.last()
	for _, want := range []string{"meituan.png 识别结果（支付）", "1. 商户  美团", "CNY 61.46", "2026-09-12 20:29", "回复「确认」保存"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card missing %q:\n%s", want, card)
		}
	}
	session, ok := f.session(t)
	if !ok || session.State != StateAwaitingDecision || session.JobID == "" {
		t.Fatalf("session = %#v, ok=%v", session, ok)
	}
	if f.facts(t) != 0 {
		t.Fatal("receipt must not create a fact on its own")
	}

	reply := f.dialogue.Handle(ctx, domain.ChatPlatformDingTalk, f.staffID, "确认", f.tenant.TenantID)
	if !strings.HasPrefix(reply, "已保存。") || !strings.Contains(reply, "美团") {
		t.Fatalf("confirm reply = %q", reply)
	}
	if f.facts(t) != 1 {
		t.Fatalf("payments after confirm = %d", f.facts(t))
	}
	if _, ok := f.session(t); ok {
		t.Fatal("session survived confirmation")
	}
	// 会话结束后再说「确认」只会得到提示，不会重复入账。
	if got := f.dialogue.Handle(ctx, domain.ChatPlatformDingTalk, f.staffID, "确认", f.tenant.TenantID); !strings.HasPrefix(got, replyNoSession) {
		t.Fatalf("post-confirm reply = %q", got)
	}
	if f.facts(t) != 1 {
		t.Fatal("second confirm created another fact")
	}
}

// 作废等同网页驳回：不生成记录，会话结束，原件一并清掉——同一份文件因此可以
// 重新投递，不会被"这份文件之前已收过"永远挡在门外。
func TestDiscardRejectsAndRemovesOriginalSoTheFileCanComeBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.deliver(t, "discard.png", 2, 0)
	if got := f.dialogue.Handle(ctx, domain.ChatPlatformDingTalk, f.staffID, "作废", f.tenant.TenantID); got != replyDiscarded {
		t.Fatalf("discard reply = %q", got)
	}
	if f.facts(t) != 0 {
		t.Fatal("discard created a fact")
	}
	if _, ok := f.session(t); ok {
		t.Fatal("session survived discard")
	}
	var documents int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM documents`).Scan(&documents); err != nil {
		t.Fatal(err)
	}
	if documents != 0 {
		t.Fatalf("documents after discard = %d, want the original gone", documents)
	}
	// 同样的字节可以再投一次，并且重新走到等待确认。
	f.deliver(t, "discard.png", 2, 0)
	session, ok := f.session(t)
	if !ok || session.State != StateAwaitingDecision {
		t.Fatalf("resend after discard = %#v, ok=%v", session, ok)
	}
}

// 看不懂的回复不做任何决定，只把可选项再说一遍。
func TestUnparsedReplyChangesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.deliver(t, "unparsed.png", 3, 0)
	if got := f.dialogue.Handle(ctx, domain.ChatPlatformDingTalk, f.staffID, "随便说点什么", f.tenant.TenantID); got != replyUnparsed {
		t.Fatalf("unparsed reply = %q", got)
	}
	if _, ok := f.session(t); !ok {
		t.Fatal("unparsed reply ended the session")
	}
	if f.facts(t) != 0 {
		t.Fatal("unparsed reply created a fact")
	}
}

// 一人同时只有一条进行中：新单据打断旧的，并在卡片上说清旧的去哪了。
func TestNewDocumentInterruptsPreviousSession(t *testing.T) {
	f := newFixture(t)
	f.deliver(t, "first.png", 4, 0)
	first, _ := f.session(t)
	f.deliver(t, "second.png", 5, 0)

	card := f.notifier.last()
	if !strings.HasPrefix(card, "上一份 first.png 未确认，已留在网页待审核。") {
		t.Fatalf("interrupt card = %q", card)
	}
	second, ok := f.session(t)
	if !ok || second.JobID == first.JobID || second.DocumentName != "second.png" {
		t.Fatalf("session after interrupt = %#v", second)
	}
}

// 20 分钟提醒一次，30 分钟收尾；单据始终留在网页待审核队列，超时不做业务决定。
func TestSweepRemindsOnceThenExpires(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.deliver(t, "slow.png", 6, 0)
	before := len(f.notifier.sent)

	if err := f.dialogue.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.notifier.sent) != before {
		t.Fatal("swept too early")
	}

	f.clock.advance(RemindAfter + time.Minute)
	if err := f.dialogue.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.notifier.last(), "slow.png 还没确认") {
		t.Fatalf("reminder = %q", f.notifier.last())
	}
	reminded := len(f.notifier.sent)
	// 只提醒这一次。
	if err := f.dialogue.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.notifier.sent) != reminded {
		t.Fatalf("reminded twice: %#v", f.notifier.sent)
	}

	f.clock.advance(ExpireAfter - RemindAfter)
	if err := f.dialogue.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.notifier.last(), "已留在网页待审核队列") {
		t.Fatalf("expiry = %q", f.notifier.last())
	}
	if _, ok := f.session(t); ok {
		t.Fatal("session survived expiry")
	}
	var status string
	if err := f.store.DB().QueryRow(`SELECT status FROM processing_jobs ORDER BY created_at DESC LIMIT 1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.JobNeedsReview) {
		t.Fatalf("job status after expiry = %q, want needs_review", status)
	}
}

func (f fixture) say(t *testing.T, text string) string {
	t.Helper()
	return f.dialogue.Handle(context.Background(), domain.ChatPlatformDingTalk, f.staffID, text, f.tenant.TenantID)
}

func (f fixture) paymentAmount(t *testing.T) int64 {
	t.Helper()
	var amount int64
	if err := f.store.DB().QueryRow(`SELECT amount_minor FROM payments WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	return amount
}

// 回编号改字段：改完回到同一张卡（编号不变），再等确认。金额与时间都有固定写法，
// 写错当场说清该怎么写，不猜也不四舍五入。
func TestEditFieldByNumberThenConfirm(t *testing.T) {
	f := newFixture(t)
	f.deliver(t, "edit.png", 11, 0)

	if got := f.say(t, "3"); !strings.Contains(got, "把「金额」改成什么？") || !strings.Contains(got, "CNY 61.46") {
		t.Fatalf("amount prompt = %q", got)
	}
	if got := f.say(t, "不是数字"); !strings.Contains(got, "12.34") {
		t.Fatalf("bad amount reply = %q", got)
	}
	card := f.say(t, "99.90")
	if !strings.HasPrefix(card, "金额 → CNY 99.90。") || !strings.Contains(card, "3. 金额  CNY 99.90") {
		t.Fatalf("card after amount edit = %q", card)
	}

	if got := f.say(t, "4"); !strings.Contains(got, "年-月-日 时:分") {
		t.Fatalf("time prompt = %q", got)
	}
	if got := f.say(t, "9/10"); !strings.Contains(got, "2026-09-10 12:30") {
		t.Fatalf("bad time reply = %q", got)
	}
	card = f.say(t, "2026-09-10 12:30")
	if !strings.Contains(card, "4. 时间  2026-09-10 12:30") {
		t.Fatalf("card after time edit = %q", card)
	}

	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("confirm = %q", got)
	}
	if got := f.paymentAmount(t); got != 9990 {
		t.Fatalf("stored amount = %d, want 9990", got)
	}
	var businessDate string
	if err := f.store.DB().QueryRow(`SELECT business_date::text FROM payments WHERE deleted_at IS NULL`).Scan(&businessDate); err != nil {
		t.Fatal(err)
	}
	if businessDate != "2026-09-10" {
		t.Fatalf("business date = %q, want the edited day", businessDate)
	}
}

// 疑似重复逐笔判断：判为独立记录后才能确认；判为同一笔就等于作废。
func TestResolveDuplicateThenConfirm(t *testing.T) {
	f := newFixture(t)
	f.deliver(t, "first.png", 21, 0)
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("first confirm = %q", got)
	}
	// 同一图案、只差一个像素：字节不同，视觉判为近似。
	// 同一张票重投会同时触发两类候选：近似文件与字段组合，逐笔判断。
	f.deliver(t, "again.png", 21, 7)
	card := f.notifier.last()
	if !strings.Contains(card, "笔疑似重复") || !strings.Contains(card, "回复「处理」") {
		t.Fatalf("duplicate card = %q", card)
	}
	if got := f.say(t, "确认"); got != replyNeedsResolve {
		t.Fatalf("confirm before resolving = %q", got)
	}
	if got := f.say(t, "处理"); !strings.Contains(got, "这是同一笔吗？") {
		t.Fatalf("duplicate question = %q", got)
	}
	if got := f.say(t, "说不清"); !strings.Contains(got, "同一笔") {
		t.Fatalf("unparsed duplicate answer = %q", got)
	}
	card = f.say(t, "不是")
	for strings.Contains(card, "这是同一笔吗？") {
		card = f.say(t, "不是")
	}
	if !strings.Contains(card, "疑似重复为独立记录") || !strings.Contains(card, "回复「确认」保存") {
		t.Fatalf("card after duplicates = %q", card)
	}
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("second confirm = %q", got)
	}
	var payments int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM payments WHERE deleted_at IS NULL`).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 2 {
		t.Fatalf("payments = %d, want both kept", payments)
	}
}

func TestDuplicateJudgedSameDiscardsTheDocument(t *testing.T) {
	f := newFixture(t)
	f.deliver(t, "first.png", 22, 0)
	f.say(t, "确认")
	f.deliver(t, "again.png", 22, 9)
	if got := f.say(t, "处理"); !strings.Contains(got, "这是同一笔吗？") {
		t.Fatalf("duplicate question = %q", got)
	}
	if got := f.say(t, "同一笔"); got != replyDiscarded {
		t.Fatalf("same-payment answer = %q", got)
	}
	var payments int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM payments WHERE deleted_at IS NULL`).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 1 {
		t.Fatalf("payments = %d, want the duplicate discarded", payments)
	}
	if _, ok := f.session(t); ok {
		t.Fatal("session survived discard")
	}
}

// 关联候选：挑一张时自动按可分配额度关联，不再多问一句。
func TestLinkSingleCandidateInChat(t *testing.T) {
	f := newFixture(t)
	f.deliver(t, "payment.png", 31, 0)
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("payment confirm = %q", got)
	}
	*f.envelope = invoiceEnvelope("61.46", "美团", "INV-0001")
	f.deliver(t, "invoice.png", 32, 0)
	card := f.notifier.last()
	if !strings.Contains(card, "找到 1 张可关联的单据") {
		t.Fatalf("invoice card = %q", card)
	}
	choices := f.say(t, "处理")
	if !strings.Contains(choices, "可关联的单据：") || !strings.Contains(choices, "1. ") {
		t.Fatalf("candidate list = %q", choices)
	}
	card = f.say(t, "1")
	if !strings.HasPrefix(card, "已关联 ") || !strings.Contains(card, "CNY 61.46") {
		t.Fatalf("card after linking = %q", card)
	}
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("invoice confirm = %q", got)
	}
	var links int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM payment_invoice_links WHERE ended_at IS NULL`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if links != 1 {
		t.Fatalf("allocation links = %d, want 1", links)
	}
}

// 明确「不关联」也是一种决定：记下来，确认时按"拒绝全部候选"提交。
func TestRejectAllCandidatesInChat(t *testing.T) {
	f := newFixture(t)
	f.deliver(t, "payment.png", 41, 0)
	f.say(t, "确认")
	*f.envelope = invoiceEnvelope("61.46", "美团", "INV-0002")
	f.deliver(t, "invoice.png", 42, 0)
	f.say(t, "处理")
	card := f.say(t, "不关联")
	if !strings.Contains(card, "· 不关联任何单据") {
		t.Fatalf("card after rejecting = %q", card)
	}
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("confirm = %q", got)
	}
	var links int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM payment_invoice_links WHERE ended_at IS NULL`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if links != 0 {
		t.Fatalf("allocation links = %d, want none", links)
	}
}

// 菜单按当前状态给出能做的事；漏票与待审核是聊天里唯二的查询，口径来自同一套用例。
func TestMenuAndQueries(t *testing.T) {
	f := newFixture(t)

	// 还没有单据时：只给通用选项，并提示菜单的存在。
	if got := f.say(t, "菜单"); !strings.Contains(got, "直接发支付截图或发票") ||
		strings.Contains(got, "当前在处理") {
		t.Fatalf("idle menu = %q", got)
	}
	if got := f.say(t, "随便说点什么"); !strings.Contains(got, "回复「菜单」") {
		t.Fatalf("idle hint = %q", got)
	}
	if got := f.say(t, "待审核"); got != "没有待审核的单据。" {
		t.Fatalf("idle pending = %q", got)
	}
	if got := f.say(t, "漏票"); got != "没有缺发票的支付，都齐了。" {
		t.Fatalf("idle gap = %q", got)
	}

	f.deliver(t, "menu.png", 51, 0)
	// 有单据时：先说这份单据怎么处理。
	menu := f.say(t, "菜单")
	if !strings.Contains(menu, "当前在处理：menu.png") || !strings.Contains(menu, "· 确认") ||
		!strings.Contains(menu, "· 编号") || !strings.Contains(menu, "· 漏票") {
		t.Fatalf("session menu = %q", menu)
	}
	if got := f.say(t, "待审核"); !strings.Contains(got, "还有 1 份待审核") || !strings.Contains(got, "menu.png") {
		t.Fatalf("pending with session = %q", got)
	}
	// 查询不动会话：问完还能接着确认。
	if session, ok := f.session(t); !ok || session.State != StateAwaitingDecision {
		t.Fatalf("session after queries = %#v", session)
	}
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("confirm after queries = %q", got)
	}
	// 确认后这笔支付没有发票，漏票查询要认出它。
	gap := f.say(t, "漏票")
	if !strings.Contains(gap, "还缺发票的支付：") || !strings.Contains(gap, "美团") || !strings.Contains(gap, "CNY 61.46") {
		t.Fatalf("gap report = %q", gap)
	}
}

// 多张候选要逐张问金额：本单剩余与每张的可接额度都不能超，写错当场挡住。
func TestLinkMultipleCandidatesAsksEachAmount(t *testing.T) {
	f := newFixture(t)
	f.deliver(t, "payment-a.png", 61, 0)
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("first payment = %q", got)
	}
	*f.envelope = paymentEnvelope("20.00", "另一家店")
	f.deliver(t, "payment-b.png", 62, 0)
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("second payment = %q", got)
	}
	// 一张发票同时够得着两笔支付：两张候选。
	*f.envelope = invoiceEnvelope("81.46", "美团", "INV-MULTI")
	f.deliver(t, "invoice.png", 63, 0)
	if card := f.notifier.last(); !strings.Contains(card, "找到 2 张可关联的单据") {
		t.Fatalf("invoice card = %q", card)
	}
	if got := f.say(t, "处理"); !strings.Contains(got, "可关联的单据：") {
		t.Fatalf("candidate list = %q", got)
	}
	if got := f.say(t, "1,9"); !strings.Contains(got, "回复编号关联") {
		t.Fatalf("out-of-range choice = %q", got)
	}
	ask := f.say(t, "1,2")
	if !strings.Contains(ask, "分配多少？") || !strings.Contains(ask, "本单还剩 CNY 81.46") {
		t.Fatalf("first amount prompt = %q", ask)
	}
	if got := f.say(t, "很多"); !strings.Contains(got, "12.34") {
		t.Fatalf("bad amount = %q", got)
	}
	if got := f.say(t, "999.00"); !strings.Contains(got, "不超过") {
		t.Fatalf("over-limit amount = %q", got)
	}
	second := f.say(t, "61.46")
	if !strings.Contains(second, "已分配 CNY 61.46") || !strings.Contains(second, "本单还剩 CNY 20.00") {
		t.Fatalf("second amount prompt = %q", second)
	}
	card := f.say(t, "20.00")
	if !strings.Contains(card, "· 关联 ") || !strings.Contains(card, "CNY 61.46") || !strings.Contains(card, "CNY 20.00") {
		t.Fatalf("card after allocating = %q", card)
	}
	if got := f.say(t, "确认"); !strings.HasPrefix(got, "已保存。") {
		t.Fatalf("confirm = %q", got)
	}
	var links int
	var allocated int64
	if err := f.store.DB().QueryRow(`SELECT count(*), coalesce(sum(allocated_minor), 0) FROM payment_invoice_links WHERE ended_at IS NULL`).Scan(&links, &allocated); err != nil {
		t.Fatal(err)
	}
	if links != 2 || allocated != 8146 {
		t.Fatalf("links = %d, allocated = %d, want 2 / 8146", links, allocated)
	}
}
