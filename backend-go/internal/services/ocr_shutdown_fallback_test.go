package services

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
)

type controlledOCRWorker struct {
	lifecycle      *RapidOCRWorker
	recognize      func(context.Context, string, string, string) ([]byte, error)
	recognizeCalls atomic.Int32
	fallbackCalls  atomic.Int32
}

func newControlledOCRWorker(
	recognize func(context.Context, string, string, string) ([]byte, error),
) *controlledOCRWorker {
	return &controlledOCRWorker{
		lifecycle: NewRapidOCRWorker(),
		recognize: recognize,
	}
}

func (w *controlledOCRWorker) StartIfEnabled() (bool, error) {
	return true, nil
}

func (w *controlledOCRWorker) Recognize(
	ctx context.Context,
	scriptPath string,
	imagePath string,
	profile string,
) ([]byte, error) {
	w.recognizeCalls.Add(1)
	return w.recognize(ctx, scriptPath, imagePath, profile)
}

func (w *controlledOCRWorker) RunFallback(
	ctx context.Context,
	fallback func(context.Context) (string, error),
) (string, error) {
	w.fallbackCalls.Add(1)
	return w.lifecycle.RunFallback(ctx, fallback)
}

func (w *controlledOCRWorker) Shutdown(ctx context.Context) error {
	return w.lifecycle.Shutdown(ctx)
}

type ocrRecognitionResult struct {
	text string
	err  error
}

func TestOCRServiceDoesNotFallbackForTerminalWorkerErrors(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{name: "worker closed", err: ErrOCRWorkerClosed},
		{name: "request canceled", err: context.Canceled},
		{name: "request deadline", err: context.DeadlineExceeded},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("SBM_OCR_WORKER", "1")
			prepareOCRFallbackScripts(t)

			worker := newControlledOCRWorker(func(context.Context, string, string, string) ([]byte, error) {
				return nil, testCase.err
			})
			service := NewOCRServiceWithWorker(worker)
			var cliCalls atomic.Int32
			service.cliRunner = func(context.Context, string, string, []string) ([]byte, error) {
				cliCalls.Add(1)
				return successfulOCRCLIOutput(), nil
			}

			_, err := service.RecognizeWithRapidOCR("unused.png")
			if !errors.Is(err, testCase.err) {
				t.Fatalf("OCR 错误 = %v，期望 %v", err, testCase.err)
			}
			if worker.fallbackCalls.Load() != 0 || cliCalls.Load() != 0 {
				t.Fatalf("终态错误后启动了 fallback：lifecycle=%d cli=%d", worker.fallbackCalls.Load(), cliCalls.Load())
			}
		})
	}
}

func TestOCRServiceShutdownWinsBeforeInvalidWorkerResultFallback(t *testing.T) {
	t.Setenv("SBM_OCR_WORKER", "1")
	prepareOCRFallbackScripts(t)

	worker := newControlledOCRWorker(func(context.Context, string, string, string) ([]byte, error) {
		return []byte("not-json"), nil
	})
	service := NewOCRServiceWithWorker(worker)
	var cliCalls atomic.Int32
	service.cliRunner = func(context.Context, string, string, []string) ([]byte, error) {
		cliCalls.Add(1)
		return successfulOCRCLIOutput(), nil
	}

	hookReached := make(chan struct{})
	releaseHook := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseHook:
		default:
			close(releaseHook)
		}
	})
	service.beforeWorkerFallback = func() {
		close(hookReached)
		<-releaseHook
	}

	result := make(chan ocrRecognitionResult, 1)
	go func() {
		text, err := service.RecognizeWithRapidOCR("unused.png")
		result <- ocrRecognitionResult{text: text, err: err}
	}()
	waitForTestValue(t, hookReached, "worker 返回无效结果后的 fallback 闸门")

	shutdownErr := worker.Shutdown(context.Background())
	close(releaseHook)
	if shutdownErr != nil {
		t.Fatalf("关闭 OCR worker 失败: %v", shutdownErr)
	}
	recognition := waitForTestValue(t, result, "关闭后的 OCR 结果")
	if !errors.Is(recognition.err, ErrOCRWorkerClosed) {
		t.Fatalf("关闭后的 OCR 错误 = %v，期望 ErrOCRWorkerClosed", recognition.err)
	}
	if cliCalls.Load() != 0 {
		t.Fatalf("Shutdown 已开始后仍启动了 %d 次 CLI", cliCalls.Load())
	}
}

func TestOCRServiceShutdownCancelsAndWaitsForInFlightCLI(t *testing.T) {
	t.Setenv("SBM_OCR_WORKER", "1")
	prepareOCRFallbackScripts(t)

	worker := newControlledOCRWorker(func(context.Context, string, string, string) ([]byte, error) {
		return nil, errors.New("worker unavailable")
	})
	service := NewOCRServiceWithWorker(worker)
	cliStarted := make(chan struct{})
	cliCanceled := make(chan struct{})
	releaseCLI := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseCLI:
		default:
			close(releaseCLI)
		}
	})
	service.cliRunner = func(ctx context.Context, _ string, _ string, _ []string) ([]byte, error) {
		close(cliStarted)
		<-ctx.Done()
		close(cliCanceled)
		<-releaseCLI
		return nil, ctx.Err()
	}

	recognitionResult := make(chan ocrRecognitionResult, 1)
	go func() {
		text, err := service.RecognizeWithRapidOCR("unused.png")
		recognitionResult <- ocrRecognitionResult{text: text, err: err}
	}()
	waitForTestValue(t, cliStarted, "CLI fallback 启动")

	shutdownResult := make(chan error, 1)
	go func() {
		shutdownResult <- worker.Shutdown(context.Background())
	}()
	waitForTestValue(t, cliCanceled, "Shutdown 取消在途 CLI fallback")
	select {
	case err := <-shutdownResult:
		t.Fatalf("CLI 尚未回收时 Shutdown 提前返回: %v", err)
	default:
	}

	close(releaseCLI)
	if err := waitForTestValue(t, shutdownResult, "CLI 回收后的 Shutdown"); err != nil {
		t.Fatalf("关闭 OCR worker 失败: %v", err)
	}
	recognition := waitForTestValue(t, recognitionResult, "取消后的 OCR 结果")
	if !errors.Is(recognition.err, ErrOCRWorkerClosed) {
		t.Fatalf("取消后的 OCR 错误 = %v，期望 ErrOCRWorkerClosed", recognition.err)
	}
}

func TestOCRServiceWorkerDisabledStillRunsDirectCLI(t *testing.T) {
	t.Setenv("SBM_OCR_WORKER", "0")
	prepareOCRFallbackScripts(t)

	worker := newControlledOCRWorker(func(context.Context, string, string, string) ([]byte, error) {
		return nil, errors.New("worker must not run")
	})
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("预先关闭测试 worker 失败: %v", err)
	}
	service := NewOCRServiceWithWorker(worker)
	var cliCalls atomic.Int32
	service.cliRunner = func(context.Context, string, string, []string) ([]byte, error) {
		cliCalls.Add(1)
		return successfulOCRCLIOutput(), nil
	}

	text, err := service.RecognizeWithRapidOCR("unused.png")
	if err != nil {
		t.Fatalf("worker 未启用时直接 CLI 失败: %v", err)
	}
	if text != "fallback text" {
		t.Fatalf("直接 CLI 文本 = %q，期望 fallback text", text)
	}
	if worker.recognizeCalls.Load() != 0 || worker.fallbackCalls.Load() != 0 || cliCalls.Load() != 1 {
		t.Fatalf(
			"worker 未启用时调用计数异常：recognize=%d lifecycle=%d cli=%d",
			worker.recognizeCalls.Load(),
			worker.fallbackCalls.Load(),
			cliCalls.Load(),
		)
	}
}

func TestOCRServiceActiveWorkerFailuresStillFallback(t *testing.T) {
	testCases := []struct {
		name   string
		output []byte
		err    error
	}{
		{name: "ordinary error", err: errors.New("worker unavailable")},
		{name: "invalid result", output: []byte("not-json")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("SBM_OCR_WORKER", "1")
			prepareOCRFallbackScripts(t)

			worker := newControlledOCRWorker(func(context.Context, string, string, string) ([]byte, error) {
				return testCase.output, testCase.err
			})
			service := NewOCRServiceWithWorker(worker)
			var cliCalls atomic.Int32
			service.cliRunner = func(context.Context, string, string, []string) ([]byte, error) {
				cliCalls.Add(1)
				return successfulOCRCLIOutput(), nil
			}

			text, err := service.RecognizeWithRapidOCR("unused.png")
			if err != nil {
				t.Fatalf("应用运行时 worker 故障没有成功 fallback: %v", err)
			}
			if text != "fallback text" {
				t.Fatalf("fallback 文本 = %q，期望 fallback text", text)
			}
			if worker.recognizeCalls.Load() != 1 || worker.fallbackCalls.Load() != 1 || cliCalls.Load() != 1 {
				t.Fatalf(
					"worker 故障后的调用计数异常：recognize=%d lifecycle=%d cli=%d",
					worker.recognizeCalls.Load(),
					worker.fallbackCalls.Load(),
					cliCalls.Load(),
				)
			}
		})
	}
}

func prepareOCRFallbackScripts(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.Mkdir("scripts", 0o755); err != nil {
		t.Fatalf("创建测试脚本目录失败: %v", err)
	}
	for _, name := range []string{"ocr_worker.py", "ocr_cli.py"} {
		if err := os.WriteFile("scripts/"+name, []byte("# test script\n"), 0o600); err != nil {
			t.Fatalf("创建测试脚本 %s 失败: %v", name, err)
		}
	}
}

func successfulOCRCLIOutput() []byte {
	return []byte(`{"success":true,"text":"fallback text","line_count":1,"engine":"rapidocr"}`)
}
