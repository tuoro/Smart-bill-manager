package chatintake

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

var pixelPNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
)

// 内容不同的第二份原件。同租户按 SHA 去重，用同一份内容会先撞上去重，测不到
// 想测的那条判定。
var otherPixelPNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR42mM4IacBAALAAQ/kvVIVAAAAAElFTkSuQmCC",
)

type fixture struct {
	service Service
	store   *postgresqladapter.Store
	owner   ports.BootstrapOwner
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	store := postgresqltest.Open(t)
	owner := ports.BootstrapOwner{
		UserID:          "00000000-0000-4000-8000-000000000101",
		TenantID:        "00000000-0000-4000-8000-000000000102",
		Email:           "owner@example.test",
		PasswordHash:    "test-only",
		DisplayName:     "Owner",
		TenantName:      "Tenant",
		DefaultCurrency: domain.CurrencyCNY,
		Timezone:        "UTC",
		CreatedAt:       time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
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
	uploads := documents.NewUploadService(
		objects,
		inspector,
		store,
		system.IDGenerator{},
		fixedClock{now: time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)},
	)
	return fixture{service: NewService(store, uploads), store: store, owner: owner}
}

func (f fixture) link(t *testing.T, externalUserID string) {
	t.Helper()
	_, err := f.store.DB().Exec(
		`INSERT INTO chat_identities
		(platform, external_user_id, tenant_id, user_id, created_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domain.ChatPlatformDingTalk,
		externalUserID,
		f.owner.TenantID,
		f.owner.UserID,
		f.owner.UserID,
		time.Date(2026, 9, 10, 0, 30, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
}

// 直接建第二个成员：租户里只剩一个在册 owner 时数据库不允许停用他，而停用
// 正是这条用例要验的东西。
func (f fixture) addMember(t *testing.T, userID, email string, role domain.Role) {
	t.Helper()
	now := time.Date(2026, 9, 10, 0, 20, 0, 0, time.UTC)
	if _, err := f.store.DB().Exec(
		`INSERT INTO users (id, email, password_hash, display_name, created_at, updated_at)
		VALUES (?, ?, 'test-only', 'Member', ?, ?)`,
		userID, email, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().Exec(
		`INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`,
		f.owner.TenantID, userID, string(role), now, now,
	); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) linkMember(t *testing.T, externalUserID, userID string) {
	t.Helper()
	_, err := f.store.DB().Exec(
		`INSERT INTO chat_identities
		(platform, external_user_id, tenant_id, user_id, created_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domain.ChatPlatformDingTalk,
		externalUserID,
		f.owner.TenantID,
		userID,
		f.owner.UserID,
		time.Date(2026, 9, 10, 0, 30, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func (f fixture) documentCount(t *testing.T) int {
	t.Helper()
	var total int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM documents`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

func message(externalUserID string) Message {
	return Message{
		Platform:       domain.ChatPlatformDingTalk,
		ExternalUserID: externalUserID,
		FileName:       "receipt.png",
		MIME:           "image/png",
		Source:         bytes.NewReader(pixelPNG),
	}
}

// 投递必须落进发送者自己所属的租户，并且带上可区分的来源标记——事后要能分清
// 哪些单据是聊天进来的，哪些是网页传的。
func TestChatIntakeCreatesDocumentAttributedToTheLinkedMember(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.link(t, "ding-user-1")

	result, err := f.service.Receive(ctx, message("ding-user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.TenantID != f.owner.TenantID || result.UserID != f.owner.UserID {
		t.Fatalf("intake attributed to %q/%q", result.TenantID, result.UserID)
	}
	if result.DocumentID == "" || result.JobID == "" {
		t.Fatalf("intake result = %#v", result)
	}
	var kind, owner, createdBy string
	err = f.store.DB().QueryRow(
		`SELECT ingestion_kind, original_object_owner, created_by_user_id FROM documents WHERE id = ?`,
		result.DocumentID,
	).Scan(&kind, &owner, &createdBy)
	if err != nil {
		t.Fatal(err)
	}
	if kind != domain.DocumentIngestionDingTalk {
		t.Fatalf("ingestion kind = %q", kind)
	}
	if owner != domain.DocumentObjectOwnerDocument {
		t.Fatalf("object owner = %q", owner)
	}
	if createdBy != f.owner.UserID {
		t.Fatalf("created by = %q", createdBy)
	}
	var queued int
	err = f.store.DB().QueryRow(
		`SELECT count(*) FROM processing_jobs WHERE document_id = ? AND status = 'queued'`,
		result.DocumentID,
	).Scan(&queued)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued jobs = %d", queued)
	}
}

// 这条通道绕开了登录会话：能给机器人发消息不等于有权往账目里投单据。未绑定的
// 发送者必须被拒，而且不能留下任何痕迹——落进某个默认租户是最坏的结果。
func TestChatIntakeRejectsUnlinkedSenderWithoutWritingAnything(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.link(t, "ding-user-1")

	for _, sender := range []string{"ding-stranger", ""} {
		if _, err := f.service.Receive(ctx, message(sender)); !errors.Is(
			err,
			domain.ErrChatSenderNotLinked,
		) {
			t.Fatalf("sender %q error = %v", sender, err)
		}
	}
	if total := f.documentCount(t); total != 0 {
		t.Fatalf("documents = %d", total)
	}
}

// 成员被停用后映射行还在，但他不该还能投件。判定放在查询里，避免第二处实现。
func TestChatIntakeStopsAcceptingAfterMembershipIsSuspended(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const memberID = "00000000-0000-4000-8000-000000000103"
	f.addMember(t, memberID, "member@example.test", domain.RoleFinance)
	f.linkMember(t, "ding-user-2", memberID)

	// 停用之前投得进，确认这条用例验的是停用本身，不是别的原因。
	if _, err := f.service.Receive(ctx, message("ding-user-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().Exec(
		`UPDATE memberships SET status = 'suspended' WHERE tenant_id = ? AND user_id = ?`,
		f.owner.TenantID,
		memberID,
	); err != nil {
		t.Fatal(err)
	}

	second := message("ding-user-2")
	second.FileName = "another.png"
	second.Source = bytes.NewReader(otherPixelPNG)
	if _, err := f.service.Receive(ctx, second); !errors.Is(
		err,
		domain.ErrChatSenderNotLinked,
	) {
		t.Fatalf("suspended member error = %v", err)
	}
	// 停用前那份留着，停用后没有新增。
	if total := f.documentCount(t); total != 1 {
		t.Fatalf("documents = %d", total)
	}
}

// 通道重投同一条消息是常态。复用既有的同租户 SHA 去重，因此不需要第二套幂等
// 机制；第二次必须报重复并指回同一份单据，而不是又建一份。
func TestChatIntakeIsIdempotentForRedeliveredFiles(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.link(t, "ding-user-1")

	first, err := f.service.Receive(ctx, message("ding-user-1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.Receive(ctx, message("ding-user-1"))
	var duplicate *domain.DuplicateDocumentError
	if !errors.As(err, &duplicate) {
		t.Fatalf("redelivery error = %v", err)
	}
	if duplicate.DocumentID != first.DocumentID {
		t.Fatalf("duplicate points at %q, want %q", duplicate.DocumentID, first.DocumentID)
	}
	if total := f.documentCount(t); total != 1 {
		t.Fatalf("documents = %d", total)
	}
}

// 一个外部账号最多对应一个成员：机器人收到文件时必须能唯一确定归属，歧义在
// 收单路径上不可接受。这条由主键保证，测试盯住它不被后续迁移放宽。
func TestChatIdentityCannotMapOneAccountToTwoMembers(t *testing.T) {
	f := newFixture(t)
	f.link(t, "ding-user-1")
	_, err := f.store.DB().Exec(
		`INSERT INTO chat_identities
		(platform, external_user_id, tenant_id, user_id, created_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domain.ChatPlatformDingTalk,
		"ding-user-1",
		f.owner.TenantID,
		f.owner.UserID,
		f.owner.UserID,
		time.Date(2026, 9, 10, 0, 40, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("second mapping for the same account was accepted")
	}
}

func TestChatIntakeRejectsUnsupportedPlatform(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.link(t, "ding-user-1")

	unsupported := message("ding-user-1")
	unsupported.Platform = "wecom"
	if _, err := f.service.Receive(ctx, unsupported); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("unsupported platform error = %v", err)
	}
	if total := f.documentCount(t); total != 0 {
		t.Fatalf("documents = %d", total)
	}
}
