//go:build cgo

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"smart-bill-manager/internal/migrations"
	"smart-bill-manager/internal/services"
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

func TestApplicationWaitsForInjectedOCRWorkerShutdown(t *testing.T) {
	t.Setenv("SBM_DRAFT_TTL_HOURS", "0")
	worker := &blockingOCRWorker{
		shutdownStarted: make(chan struct{}),
		releaseShutdown: make(chan struct{}),
	}
	application := newTestApplicationWithOCRWorker(t, worker)
	ctx, cancel := context.WithCancel(context.Background())
	application.Start(ctx)
	cancel()

	select {
	case <-worker.shutdownStarted:
	case <-time.After(time.Second):
		t.Fatal("应用取消后未开始关闭注入的 OCR worker")
	}

	earlyWaitCtx, earlyWaitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer earlyWaitCancel()
	if err := application.Wait(earlyWaitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("OCR worker 尚未关闭时 Application.Wait 应继续等待，实际错误: %v", err)
	}

	close(worker.releaseShutdown)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if err := application.Wait(waitCtx); err != nil {
		t.Fatalf("OCR worker 关闭后应用未完成停机: %v", err)
	}
}

func newTestApplication(t *testing.T) *Application {
	return newTestApplicationWithOCRWorker(t, nil)
}

func newTestApplicationWithOCRWorker(t *testing.T, worker services.OCRWorker) *Application {
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

	var application *Application
	if worker == nil {
		application, err = New(testConfig(t), db, t.TempDir())
	} else {
		application, err = NewWithOCRWorker(testConfig(t), db, t.TempDir(), worker)
	}
	if err != nil {
		t.Fatalf("创建测试应用失败: %v", err)
	}
	return application
}

type blockingOCRWorker struct {
	shutdownStarted chan struct{}
	releaseShutdown chan struct{}
}

func (w *blockingOCRWorker) StartIfEnabled() (bool, error) {
	return true, nil
}

func (w *blockingOCRWorker) Recognize(context.Context, string, string, string) ([]byte, error) {
	return nil, errors.New("unexpected OCR request")
}

func (w *blockingOCRWorker) RunFallback(ctx context.Context, fallback func(context.Context) (string, error)) (string, error) {
	return fallback(ctx)
}

func (w *blockingOCRWorker) Shutdown(ctx context.Context) error {
	close(w.shutdownStarted)
	select {
	case <-w.releaseShutdown:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
