package services

import (
	"context"
	"errors"
	"os"
	"testing"
)

type closedOCRWorkerStub struct {
	recognizeCalls int
}

func (w *closedOCRWorkerStub) StartIfEnabled() (bool, error) {
	return true, nil
}

func (w *closedOCRWorkerStub) Recognize(context.Context, string, string, string) ([]byte, error) {
	w.recognizeCalls++
	return nil, ErrOCRWorkerClosed
}

func (w *closedOCRWorkerStub) Shutdown(context.Context) error {
	return nil
}

func TestOCRServiceDoesNotFallbackToCLIWhenWorkerIsClosed(t *testing.T) {
	t.Setenv("SBM_OCR_WORKER", "1")
	t.Chdir(t.TempDir())
	if err := os.Mkdir("scripts", 0o755); err != nil {
		t.Fatalf("创建测试脚本目录失败: %v", err)
	}
	if err := os.WriteFile("scripts/ocr_worker.py", []byte("# test worker\n"), 0o600); err != nil {
		t.Fatalf("创建测试 worker 脚本失败: %v", err)
	}

	worker := &closedOCRWorkerStub{}
	service := NewOCRServiceWithWorker(worker)
	_, err := service.RecognizeWithRapidOCR("unused.png")
	if !errors.Is(err, ErrOCRWorkerClosed) {
		t.Fatalf("关闭后的 OCR 错误 = %v，期望 ErrOCRWorkerClosed", err)
	}
	if worker.recognizeCalls != 1 {
		t.Fatalf("worker Recognize 调用次数 = %d，期望 1", worker.recognizeCalls)
	}
}
