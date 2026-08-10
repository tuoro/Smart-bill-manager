package services

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestRapidOCRWorkerShutdownWaitsForChildProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestRapidOCRWorkerHelperProcess$")
	cmd.Env = append(os.Environ(), "SBM_OCR_WORKER_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 OCR worker 测试子进程失败: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	waitCh := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitCh)
	}()
	worker := &RapidOCRWorker{
		process: rapidOCRWorkerProcess{
			cmd:    cmd,
			waitCh: waitCh,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := worker.Shutdown(ctx); err != nil {
		t.Fatalf("关闭 OCR worker 失败: %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("Shutdown 返回前必须等待子进程 Wait 完成")
	}
	if worker.process.cmd != nil || worker.process.waitCh != nil {
		t.Fatal("Shutdown 后不应保留子进程生命周期状态")
	}
}

func TestRapidOCRWorkerHelperProcess(t *testing.T) {
	if os.Getenv("SBM_OCR_WORKER_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}
