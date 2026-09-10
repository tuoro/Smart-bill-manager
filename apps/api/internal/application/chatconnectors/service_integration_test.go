package chatconnectors

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// 假探测：按 secret 决定通过或拒绝，并记住收到的凭据以证明解密正确。
type fakeProbe struct{ seen []string }

func (p *fakeProbe) Probe(_ context.Context, _, appKey, appSecret string) error {
	p.seen = append(p.seen, appKey+"/"+appSecret)
	if appSecret == "bad-secret" {
		return ErrCredentialsRejected
	}
	return nil
}

// 假运行时：记录起停顺序。
type fakeRuntime struct{ events []string }

func (r *fakeRuntime) Start(tenantID, platform, appKey, _ string) {
	r.events = append(r.events, "start:"+tenantID+":"+appKey)
}
func (r *fakeRuntime) Stop(tenantID, platform string) { r.events = append(r.events, "stop:"+tenantID) }

type fixture struct {
	service Service
	probe   *fakeProbe
	runtime *fakeRuntime
	tenant  domain.TenantContext
	other   domain.TenantContext
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	store := postgresqltest.Open(t)
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	owner := ports.BootstrapOwner{
		UserID: "00000000-0000-4000-8000-000000000101", TenantID: "00000000-0000-4000-8000-000000000102",
		Email: "owner@example.test", PasswordHash: "test-only", DisplayName: "Owner", TenantName: "Tenant",
		DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: now,
	}
	if err := store.BootstrapOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	// 第二个工作区，用来验 AppKey 不能被两边各认一次。
	if _, err := store.DB().Exec(`INSERT INTO tenants (id, name, default_currency, timezone, created_at, updated_at) VALUES (?, '另一个', 'CNY', 'UTC', ?, ?)`,
		"00000000-0000-4000-8000-000000000202", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO memberships (tenant_id, user_id, role, status, created_at, updated_at) VALUES (?, ?, 'owner', 'active', ?, ?)`,
		"00000000-0000-4000-8000-000000000202", owner.UserID, now, now); err != nil {
		t.Fatal(err)
	}
	cipher, err := cryptography.NewSecretCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	probe, runtime := &fakeProbe{}, &fakeRuntime{}
	return fixture{
		service: NewService(store, store, cipher, probe, runtime, fixedClock{now: now.Add(time.Hour)}),
		probe:   probe, runtime: runtime,
		tenant: domain.TenantContext{TenantID: owner.TenantID, UserID: owner.UserID, Role: domain.RoleOwner},
		other:  domain.TenantContext{TenantID: "00000000-0000-4000-8000-000000000202", UserID: owner.UserID, Role: domain.RoleOwner},
	}
}

// 保存 → 检测 → 启用是唯一路径；密文只在服务层解密，界面拿到的是「已设置」。
func TestConnectorLifecycleSaveDetectActivate(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const dt = domain.ChatPlatformDingTalk

	if _, err := f.service.Get(ctx, f.tenant, dt); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty get = %v", err)
	}
	saved, err := f.service.Save(ctx, f.tenant, dt, "  app-key-1  ", []byte("good-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.AppKey != "app-key-1" || !saved.HasSecret || saved.DetectionStatus != domain.ChatConnectorDetectionPending || saved.Active {
		t.Fatalf("saved = %#v", saved)
	}
	// 没检测就启用：拒绝。
	if _, err := f.service.Activate(ctx, f.tenant, dt); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("activate before detect = %v", err)
	}
	detected, err := f.service.Detect(ctx, f.tenant, dt)
	if err != nil {
		t.Fatal(err)
	}
	if detected.DetectionStatus != domain.ChatConnectorDetectionPassed || detected.DetectionAt == nil {
		t.Fatalf("detected = %#v", detected)
	}
	// 探测拿到的是解密后的明文，证明加密往返正确。
	if len(f.probe.seen) != 1 || f.probe.seen[0] != "app-key-1/good-secret" {
		t.Fatalf("probe saw %v", f.probe.seen)
	}
	active, err := f.service.Activate(ctx, f.tenant, dt)
	if err != nil || !active.Active {
		t.Fatalf("activate = %#v / %v", active, err)
	}
	if got := f.runtime.events; len(got) == 0 || got[len(got)-1] != "start:"+f.tenant.TenantID+":app-key-1" {
		t.Fatalf("runtime events = %v", got)
	}
}

// 换凭据即重置：检测打回 pending、启用清零、正在跑的连接停掉。新密钥没验过
// 就不该有连接在用它。
func TestSavingNewCredentialsResetsDetectionAndStopsTheConnection(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const dt = domain.ChatPlatformDingTalk
	if _, err := f.service.Save(ctx, f.tenant, dt, "k1", []byte("good-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Detect(ctx, f.tenant, dt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(ctx, f.tenant, dt); err != nil {
		t.Fatal(err)
	}
	f.runtime.events = nil
	resaved, err := f.service.Save(ctx, f.tenant, dt, "k1", []byte("rotated-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if resaved.Active || resaved.DetectionStatus != domain.ChatConnectorDetectionPending || resaved.Version != 2 {
		t.Fatalf("resaved = %#v", resaved)
	}
	if len(f.runtime.events) != 1 || f.runtime.events[0] != "stop:"+f.tenant.TenantID {
		t.Fatalf("runtime events = %v", f.runtime.events)
	}
}

// 检测失败要写回可展示的原因，且不能启用。
func TestFailedDetectionIsRecordedAndBlocksActivation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const dt = domain.ChatPlatformDingTalk
	if _, err := f.service.Save(ctx, f.tenant, dt, "k1", []byte("bad-secret")); err != nil {
		t.Fatal(err)
	}
	detected, err := f.service.Detect(ctx, f.tenant, dt)
	if err != nil {
		t.Fatal(err)
	}
	if detected.DetectionStatus != domain.ChatConnectorDetectionFailed || detected.DetectionMessage != "钉钉拒绝了这对 AppKey/AppSecret" {
		t.Fatalf("detected = %#v", detected)
	}
	if _, err := f.service.Activate(ctx, f.tenant, dt); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("activate after failure = %v", err)
	}
}

// 同一个 AppKey 不能被两个工作区各认一次：机器人收到的消息必须能唯一确定归属。
func TestTheSameAppKeyCannotBeClaimedByTwoTenants(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const dt = domain.ChatPlatformDingTalk
	if _, err := f.service.Save(ctx, f.tenant, dt, "shared-key", []byte("s")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Save(ctx, f.other, dt, "shared-key", []byte("s")); err == nil {
		t.Fatal("second tenant claimed the same app key")
	}
}

// 启动时把已启用的连接全部拉起来；未启用的不动。
func TestStartActiveOnlyStartsActivatedConnectors(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	const dt = domain.ChatPlatformDingTalk
	if _, err := f.service.Save(ctx, f.tenant, dt, "k-active", []byte("good-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Detect(ctx, f.tenant, dt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(ctx, f.tenant, dt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Save(ctx, f.other, dt, "k-idle", []byte("good-secret")); err != nil {
		t.Fatal(err)
	}
	f.runtime.events = nil
	if err := f.service.StartActive(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.runtime.events) != 1 || f.runtime.events[0] != "start:"+f.tenant.TenantID+":k-active" {
		t.Fatalf("runtime events = %v", f.runtime.events)
	}
}

// 只有能管集成配置的角色才能碰凭据。
func TestConnectorRequiresProviderManagementCapability(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	viewer := domain.TenantContext{TenantID: f.tenant.TenantID, UserID: f.tenant.UserID, Role: domain.RoleViewer}
	if _, err := f.service.Save(ctx, viewer, domain.ChatPlatformDingTalk, "k", []byte("s")); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer save = %v", err)
	}
}
