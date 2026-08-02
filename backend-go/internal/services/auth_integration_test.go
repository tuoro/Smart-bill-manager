//go:build cgo

package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"smart-bill-manager/internal/migrations"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

func TestAuthServiceUsesInjectedDatabase(t *testing.T) {
	primaryDB := openServiceTestDB(t)
	globalDB := openServiceTestDB(t)
	if database.GetDB() != globalDB {
		t.Fatal("测试前提不成立：全局连接应指向第二个数据库")
	}

	service := newAuthTestService(t, primaryDB)
	result, err := service.CreateInitialAdminCtx(context.Background(), "admin", "secret12", nil)
	if err != nil {
		t.Fatalf("创建初始管理员失败: %v", err)
	}
	if !result.Success || result.User == nil || result.User.Role != "admin" {
		t.Fatalf("初始管理员响应异常: %#v", result)
	}

	assertUserCount(t, primaryDB, 1)
	assertUserCount(t, globalDB, 0)

	second, err := service.CreateInitialAdminCtx(context.Background(), "admin2", "secret12", nil)
	if err != nil {
		t.Fatalf("重复初始化应返回业务结果，而不是数据库错误: %v", err)
	}
	if second.Success {
		t.Fatal("已有用户时不应再次创建初始管理员")
	}
	assertUserCount(t, primaryDB, 1)
}

func TestInviteRegistrationConsumesInviteAtomically(t *testing.T) {
	db := openServiceTestDB(t)
	service := newAuthTestService(t, db)
	admin, err := service.CreateInitialAdminCtx(context.Background(), "admin", "secret12", nil)
	if err != nil || !admin.Success || admin.User == nil {
		t.Fatalf("准备管理员失败: result=%#v err=%v", admin, err)
	}

	invite, err := service.CreateInviteCtx(context.Background(), admin.User.ID, 7)
	if err != nil {
		t.Fatalf("创建邀请码失败: %v", err)
	}
	registered, err := service.RegisterWithInviteCtx(context.Background(), invite.Code, "member", "secret12", nil)
	if err != nil {
		t.Fatalf("邀请码注册失败: %v", err)
	}
	if !registered.Success || registered.User == nil {
		t.Fatalf("邀请码注册结果异常: %#v", registered)
	}

	var storedInvite models.Invite
	if err := db.Where("code_hash = ?", inviteCodeHash(normalizeInviteCode(invite.Code))).First(&storedInvite).Error; err != nil {
		t.Fatalf("读取邀请码失败: %v", err)
	}
	if storedInvite.UsedAt == nil || storedInvite.UsedBy == nil || *storedInvite.UsedBy != registered.User.ID {
		t.Fatalf("邀请码消费状态异常: %#v", storedInvite)
	}

	reused, err := service.RegisterWithInviteCtx(context.Background(), invite.Code, "member2", "secret12", nil)
	if err != nil {
		t.Fatalf("重复使用邀请码应返回业务结果: %v", err)
	}
	if reused.Success {
		t.Fatal("已消费的邀请码不应再次注册用户")
	}
	assertUserCount(t, db, 2)
}

func TestCreateInitialAdminSerializesConcurrentRequests(t *testing.T) {
	db := openServiceTestDB(t)
	service := newAuthTestService(t, db)
	type callResult struct {
		result *AuthResult
		err    error
	}
	results := make(chan callResult, 2)
	var callers sync.WaitGroup
	for _, username := range []string{"admin-a", "admin-b"} {
		username := username
		callers.Add(1)
		go func() {
			defer callers.Done()
			result, err := service.CreateInitialAdminCtx(context.Background(), username, "secret12", nil)
			results <- callResult{result: result, err: err}
		}()
	}
	callers.Wait()
	close(results)

	successes := 0
	for call := range results {
		if call.err != nil {
			t.Fatalf("并发初始化不应产生数据库错误: %v", call.err)
		}
		if call.result.Success {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("并发初始化应仅成功一次，实际成功 %d 次", successes)
	}
	assertUserCount(t, db, 1)
}

func TestCreateInviteHonorsCanceledContext(t *testing.T) {
	db := openServiceTestDB(t)
	service := newAuthTestService(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.CreateInviteCtx(ctx, "admin-id", 7); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消请求应返回 context.Canceled，实际为 %v", err)
	}
}

func TestLoginPropagatesDatabaseCancellation(t *testing.T) {
	db := openServiceTestDB(t)
	service := newAuthTestService(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.LoginCtx(ctx, "missing", "secret12"); !errors.Is(err, context.Canceled) {
		t.Fatalf("登录查询取消应返回 context.Canceled，实际为 %v", err)
	}
}

func newAuthTestService(t *testing.T, db *gorm.DB) *AuthService {
	t.Helper()
	tokenManager, err := utils.NewTokenManager("0123456789abcdef0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatalf("创建测试令牌管理器失败: %v", err)
	}
	return NewAuthService(db, tokenManager)
}

func openServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取测试数据库句柄失败: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("关闭测试数据库失败: %v", err)
		}
	})
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	return db
}

func assertUserCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.User{}).Count(&count).Error; err != nil {
		t.Fatalf("统计用户失败: %v", err)
	}
	if count != want {
		t.Fatalf("用户数应为 %d，实际为 %d", want, count)
	}
}
