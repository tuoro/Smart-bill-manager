//go:build cgo

package app

import (
	"context"
	"testing"
	"time"

	"smart-bill-manager/internal/migrations"
	"smart-bill-manager/pkg/database"
)

func TestBackgroundWorkersStopAfterCancellation(t *testing.T) {
	t.Setenv("SBM_OCR_WORKER", "0")
	t.Setenv("SBM_DRAFT_TTL_HOURS", "0")
	application := newTestApplication(t)
	ctx, cancel := context.WithCancel(context.Background())
	application.Start(ctx)
	cancel()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if err := application.Wait(waitCtx); err != nil {
		t.Fatalf("后台任务未在超时前退出: %v", err)
	}
}

func newTestApplication(t *testing.T) *Application {
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

	application, err := New(testConfig(t), db, t.TempDir())
	if err != nil {
		t.Fatalf("创建测试应用失败: %v", err)
	}
	return application
}
