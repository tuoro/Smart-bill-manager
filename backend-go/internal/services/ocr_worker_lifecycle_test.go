package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const rapidOCRWorkerTestTimeout = 5 * time.Second

type rapidOCRWorkerCallResult struct {
	output []byte
	err    error
}

func waitForTestValue[T any](t *testing.T, ch <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(rapidOCRWorkerTestTimeout):
		t.Fatalf("等待%s超时", description)
		var zero T
		return zero
	}
}

type manualDeadlineContext struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newManualDeadlineContext() *manualDeadlineContext {
	return &manualDeadlineContext{
		Context: context.Background(),
		done:    make(chan struct{}),
	}
}

func (c *manualDeadlineContext) Done() <-chan struct{} {
	return c.done
}

func (c *manualDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (c *manualDeadlineContext) expire() {
	c.once.Do(func() {
		close(c.done)
	})
}

type observedDeadlineContext struct {
	*manualDeadlineContext
	doneObserved chan struct{}
	observeOnce  sync.Once
}

func newObservedDeadlineContext() *observedDeadlineContext {
	return &observedDeadlineContext{
		manualDeadlineContext: newManualDeadlineContext(),
		doneObserved:          make(chan struct{}),
	}
}

func (c *observedDeadlineContext) Done() <-chan struct{} {
	c.observeOnce.Do(func() {
		close(c.doneObserved)
	})
	return c.manualDeadlineContext.Done()
}

type blockingReadCloser struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	r.startOnce.Do(func() {
		close(r.started)
	})
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *blockingReadCloser) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
	})
	return nil
}

type blockingWriteCloser struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingWriteCloser() *blockingWriteCloser {
	return &blockingWriteCloser{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (w *blockingWriteCloser) Write([]byte) (int, error) {
	w.startOnce.Do(func() {
		close(w.started)
	})
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *blockingWriteCloser) Close() error {
	w.closeOnce.Do(func() {
		close(w.closed)
	})
	return nil
}

type funcWriteCloser struct {
	writeFn   func([]byte) (int, error)
	closeFn   func() error
	closeOnce sync.Once
	closeErr  error
}

func (w *funcWriteCloser) Write(payload []byte) (int, error) {
	return w.writeFn(payload)
}

func (w *funcWriteCloser) Close() error {
	w.closeOnce.Do(func() {
		if w.closeFn != nil {
			w.closeErr = w.closeFn()
		}
	})
	return w.closeErr
}

func acceptingWriteCloser() *funcWriteCloser {
	return &funcWriteCloser{
		writeFn: func(payload []byte) (int, error) {
			return len(payload), nil
		},
	}
}

type responseReadCloser struct {
	messages  chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	current   []byte
}

func newResponseReadCloser() *responseReadCloser {
	return &responseReadCloser{
		messages: make(chan []byte, 1),
		closed:   make(chan struct{}),
	}
}

func (r *responseReadCloser) Read(target []byte) (int, error) {
	for len(r.current) == 0 {
		select {
		case message := <-r.messages:
			r.current = message
		case <-r.closed:
			return 0, io.ErrClosedPipe
		}
	}
	n := copy(target, r.current)
	r.current = r.current[n:]
	return n, nil
}

func (r *responseReadCloser) send(message []byte) error {
	message = append([]byte(nil), message...)
	select {
	case r.messages <- message:
		return nil
	case <-r.closed:
		return io.ErrClosedPipe
	}
}

func (r *responseReadCloser) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
	})
	return nil
}

func installRapidOCRWorkerRun(worker *RapidOCRWorker, run *rapidOCRWorkerRun) {
	worker.process.init()
	worker.process.mu.Lock()
	worker.process.state = rapidOCRWorkerRunning
	worker.process.run = run
	worker.process.mu.Unlock()
}

func newScriptedRapidOCRWorkerRun() (*rapidOCRWorkerRun, <-chan struct{}) {
	reader := newResponseReadCloser()
	waitCh := make(chan struct{})
	terminated := make(chan struct{})
	writer := &funcWriteCloser{
		writeFn: func(payload []byte) (int, error) {
			var request struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(payload, &request); err != nil {
				return 0, err
			}
			response, err := json.Marshal(map[string]any{
				"id":      request.ID,
				"success": true,
			})
			if err != nil {
				return 0, err
			}
			response = append(response, '\n')
			if err := reader.send(response); err != nil {
				return 0, err
			}
			return len(payload), nil
		},
	}
	run := &rapidOCRWorkerRun{
		stdin:  writer,
		stdout: bufio.NewReader(reader),
		waitCh: waitCh,
	}
	run.terminateFn = func() {
		_ = writer.Close()
		_ = reader.Close()
		close(terminated)
		close(waitCh)
	}
	return run, terminated
}

func TestRapidOCRWorkerShutdownInterruptsBlockedRecognize(t *testing.T) {
	reader := newBlockingReadCloser()
	writer := acceptingWriteCloser()
	waitCh := make(chan struct{})
	terminated := make(chan struct{})
	run := &rapidOCRWorkerRun{
		stdin:  writer,
		stdout: bufio.NewReader(reader),
		waitCh: waitCh,
	}
	run.terminateFn = func() {
		_ = writer.Close()
		_ = reader.Close()
		close(terminated)
		close(waitCh)
	}
	worker := NewRapidOCRWorker()
	worker.process.startProcess = func(string, string) (*rapidOCRWorkerRun, error) {
		return run, nil
	}

	recognizeResult := make(chan rapidOCRWorkerCallResult, 1)
	go func() {
		output, err := worker.Recognize(context.Background(), "unused.py", "image.png", "default")
		recognizeResult <- rapidOCRWorkerCallResult{output: output, err: err}
	}()
	waitForTestValue(t, reader.started, "OCR 请求进入阻塞读取")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), rapidOCRWorkerTestTimeout)
	defer cancelShutdown()
	shutdownResult := make(chan error, 1)
	go func() {
		shutdownResult <- worker.Shutdown(shutdownCtx)
	}()
	waitForTestValue(t, terminated, "Shutdown 终止正在识别的进程")
	if err := waitForTestValue(t, shutdownResult, "Shutdown 返回"); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}
	result := waitForTestValue(t, recognizeResult, "阻塞的 Recognize 返回")
	if result.output != nil {
		t.Fatalf("关闭期间 Recognize 不应返回结果: %q", result.output)
	}
	if !errors.Is(result.err, ErrOCRWorkerClosed) {
		t.Fatalf("关闭期间 Recognize 错误 = %v，期望 ErrOCRWorkerClosed", result.err)
	}
}

func TestRapidOCRWorkerShutdownDuringStartReapsLateProcess(t *testing.T) {
	reader := newBlockingReadCloser()
	writer := acceptingWriteCloser()
	waitCh := make(chan struct{})
	terminated := make(chan struct{})
	run := &rapidOCRWorkerRun{
		stdin:  writer,
		stdout: bufio.NewReader(reader),
		waitCh: waitCh,
	}
	run.terminateFn = func() {
		_ = writer.Close()
		_ = reader.Close()
		close(terminated)
		close(waitCh)
	}
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	worker := NewRapidOCRWorker()
	worker.process.startProcess = func(string, string) (*rapidOCRWorkerRun, error) {
		close(startEntered)
		<-releaseStart
		return run, nil
	}

	recognizeResult := make(chan error, 1)
	go func() {
		_, err := worker.Recognize(context.Background(), "unused.py", "image.png", "default")
		recognizeResult <- err
	}()
	waitForTestValue(t, startEntered, "OCR worker 进入启动阶段")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), rapidOCRWorkerTestTimeout)
	defer cancelShutdown()
	shutdownResult := make(chan error, 1)
	go func() {
		shutdownResult <- worker.Shutdown(shutdownCtx)
	}()
	waitForTestValue(t, worker.process.closingCh, "Shutdown 标记 closing")
	close(releaseStart)
	waitForTestValue(t, terminated, "回收关闭后才启动完成的进程")
	if err := waitForTestValue(t, shutdownResult, "启动期间的 Shutdown 返回"); err != nil {
		t.Fatalf("启动期间 Shutdown 失败: %v", err)
	}
	if err := waitForTestValue(t, recognizeResult, "启动期间的 Recognize 返回"); !errors.Is(err, ErrOCRWorkerClosed) {
		t.Fatalf("启动期间关闭后的 Recognize 错误 = %v，期望 ErrOCRWorkerClosed", err)
	}
}

func TestRapidOCRWorkerSerializesRecognizeRequests(t *testing.T) {
	run, _ := newScriptedRapidOCRWorkerRun()
	baseWriter := run.stdin
	firstWriteStarted := make(chan struct{})
	releaseFirstWrite := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() {
		releaseOnce.Do(func() {
			close(releaseFirstWrite)
		})
	}
	t.Cleanup(releaseFirst)
	var writes atomic.Int32
	run.stdin = &funcWriteCloser{
		writeFn: func(payload []byte) (int, error) {
			if writes.Add(1) == 1 {
				close(firstWriteStarted)
				<-releaseFirstWrite
			}
			return baseWriter.Write(payload)
		},
		closeFn: baseWriter.Close,
	}
	worker := NewRapidOCRWorker()
	worker.process.startProcess = func(string, string) (*rapidOCRWorkerRun, error) {
		return run, nil
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := worker.Recognize(context.Background(), "unused.py", "first.png", "default")
		firstResult <- err
	}()
	waitForTestValue(t, firstWriteStarted, "首个 Recognize 进入写入")

	secondCtx := newObservedDeadlineContext()
	secondResult := make(chan error, 1)
	go func() {
		_, err := worker.Recognize(secondCtx, "unused.py", "second.png", "default")
		secondResult <- err
	}()
	waitForTestValue(t, secondCtx.doneObserved, "第二个 Recognize 等待请求闸门")
	if writes.Load() != 1 {
		t.Fatalf("首个请求阻塞时已发生 %d 次写入，识别请求未串行化", writes.Load())
	}
	secondCtx.expire()
	if err := waitForTestValue(t, secondResult, "排队的 Recognize 响应 context"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("排队的 Recognize 错误 = %v，期望 context.DeadlineExceeded", err)
	}
	releaseFirst()
	if err := waitForTestValue(t, firstResult, "首个 Recognize 完成"); err != nil {
		t.Fatalf("首个 Recognize 失败: %v", err)
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("清理串行化测试 worker 失败: %v", err)
	}
}

func TestRapidOCRWorkerShutdownTimeoutRetainsRunForRetry(t *testing.T) {
	reader := newBlockingReadCloser()
	writer := acceptingWriteCloser()
	waitCh := make(chan struct{})
	terminated := make(chan struct{})
	var terminateCount atomic.Int32
	var closeWaitOnce sync.Once
	closeWait := func() {
		closeWaitOnce.Do(func() {
			close(waitCh)
		})
	}
	t.Cleanup(closeWait)
	run := &rapidOCRWorkerRun{
		stdin:  writer,
		stdout: bufio.NewReader(reader),
		waitCh: waitCh,
	}
	run.terminateFn = func() {
		terminateCount.Add(1)
		_ = writer.Close()
		_ = reader.Close()
		close(terminated)
	}
	worker := NewRapidOCRWorker()
	installRapidOCRWorkerRun(worker, run)

	deadlineCtx := newManualDeadlineContext()
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- worker.Shutdown(deadlineCtx)
	}()
	waitForTestValue(t, terminated, "首次 Shutdown 发出终止信号")
	deadlineCtx.expire()
	if err := waitForTestValue(t, firstResult, "首次 Shutdown 超时返回"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("首次 Shutdown 错误 = %v，期望 context.DeadlineExceeded", err)
	}

	worker.process.mu.Lock()
	retainedRun := worker.process.run
	state := worker.process.state
	worker.process.mu.Unlock()
	if retainedRun != run || state != rapidOCRWorkerClosing {
		t.Fatalf("超时后状态 = (%v, %v)，必须保留 closing 进程句柄", retainedRun == run, state)
	}

	closeWait()
	retryCtx, cancelRetry := context.WithTimeout(context.Background(), rapidOCRWorkerTestTimeout)
	defer cancelRetry()
	if err := worker.Shutdown(retryCtx); err != nil {
		t.Fatalf("再次回收 OCR worker 失败: %v", err)
	}
	worker.process.mu.Lock()
	defer worker.process.mu.Unlock()
	if worker.process.run != nil || worker.process.state != rapidOCRWorkerClosed {
		t.Fatalf("再次回收后状态 = (%v, %v)，期望 closed 且无句柄", worker.process.run, worker.process.state)
	}
	if terminateCount.Load() != 1 {
		t.Fatalf("进程终止次数 = %d，期望 1", terminateCount.Load())
	}
}

func TestRapidOCRWorkerConcurrentShutdownSharesReaping(t *testing.T) {
	reader := newBlockingReadCloser()
	writer := acceptingWriteCloser()
	waitCh := make(chan struct{})
	terminated := make(chan struct{})
	var terminateCount atomic.Int32
	run := &rapidOCRWorkerRun{
		stdin:  writer,
		stdout: bufio.NewReader(reader),
		waitCh: waitCh,
	}
	run.terminateFn = func() {
		terminateCount.Add(1)
		_ = writer.Close()
		_ = reader.Close()
		close(terminated)
	}
	worker := NewRapidOCRWorker()
	installRapidOCRWorkerRun(worker, run)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), rapidOCRWorkerTestTimeout)
	defer cancelShutdown()
	const callers = 8
	results := make(chan error, callers)
	for range callers {
		go func() {
			results <- worker.Shutdown(shutdownCtx)
		}()
	}
	waitForTestValue(t, terminated, "并发 Shutdown 发出终止信号")
	close(waitCh)
	for i := 0; i < callers; i++ {
		if err := waitForTestValue(t, results, "并发 Shutdown 返回"); err != nil {
			t.Fatalf("并发 Shutdown 失败: %v", err)
		}
	}
	if terminateCount.Load() != 1 {
		t.Fatalf("并发 Shutdown 终止进程 %d 次，期望 1 次", terminateCount.Load())
	}
}

func TestRapidOCRWorkerRejectsRequestsAfterShutdown(t *testing.T) {
	t.Setenv("SBM_OCR_WORKER", "1")
	worker := NewRapidOCRWorker()
	var starts atomic.Int32
	worker.process.startProcess = func(string, string) (*rapidOCRWorkerRun, error) {
		starts.Add(1)
		run, _ := newScriptedRapidOCRWorkerRun()
		return run, nil
	}
	if err := worker.Shutdown(context.Background()); err != nil {
		t.Fatalf("关闭空闲 OCR worker 失败: %v", err)
	}
	if _, err := worker.Recognize(context.Background(), "unused.py", "image.png", "default"); !errors.Is(err, ErrOCRWorkerClosed) {
		t.Fatalf("关闭后 Recognize 错误 = %v，期望 ErrOCRWorkerClosed", err)
	}
	if started, err := worker.StartIfEnabled(); started || !errors.Is(err, ErrOCRWorkerClosed) {
		t.Fatalf("关闭后 StartIfEnabled = (%v, %v)，期望 false 和 ErrOCRWorkerClosed", started, err)
	}
	if starts.Load() != 0 {
		t.Fatalf("关闭后请求启动了 %d 个进程，期望 0", starts.Load())
	}
}

func TestRapidOCRWorkerAutomaticRestart(t *testing.T) {
	t.Setenv("SBM_OCR_WORKER_RESTART_EVERY_N", "1")
	t.Setenv("SBM_OCR_WORKER_MAX_RSS_MB", "0")
	worker := NewRapidOCRWorker()
	var terminatedRuns []<-chan struct{}
	worker.process.startProcess = func(string, string) (*rapidOCRWorkerRun, error) {
		run, terminated := newScriptedRapidOCRWorkerRun()
		terminatedRuns = append(terminatedRuns, terminated)
		return run, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rapidOCRWorkerTestTimeout)
	defer cancel()
	first, err := worker.Recognize(ctx, "unused.py", "first.png", "default")
	if err != nil {
		t.Fatalf("首次 Recognize 失败: %v", err)
	}
	if !json.Valid(first) {
		t.Fatalf("首次 Recognize 返回无效 JSON: %q", first)
	}
	second, err := worker.Recognize(ctx, "unused.py", "second.png", "default")
	if err != nil {
		t.Fatalf("重启后的 Recognize 失败: %v", err)
	}
	if !json.Valid(second) {
		t.Fatalf("重启后的 Recognize 返回无效 JSON: %q", second)
	}
	if len(terminatedRuns) != 2 {
		t.Fatalf("启动进程数 = %d，期望自动重启后为 2", len(terminatedRuns))
	}
	select {
	case <-terminatedRuns[0]:
	default:
		t.Fatal("自动重启前必须先回收旧进程")
	}
	select {
	case <-terminatedRuns[1]:
		t.Fatal("新进程不应在正常请求后被终止")
	default:
	}
	if err := worker.Shutdown(ctx); err != nil {
		t.Fatalf("清理重启后的 OCR worker 失败: %v", err)
	}
}

func TestRapidOCRWorkerRecognizeHonorsContextDuringIO(t *testing.T) {
	tests := []struct {
		name  string
		newIO func() (io.WriteCloser, io.ReadCloser, <-chan struct{})
	}{
		{
			name: "write",
			newIO: func() (io.WriteCloser, io.ReadCloser, <-chan struct{}) {
				writer := newBlockingWriteCloser()
				return writer, newBlockingReadCloser(), writer.started
			},
		},
		{
			name: "read",
			newIO: func() (io.WriteCloser, io.ReadCloser, <-chan struct{}) {
				reader := newBlockingReadCloser()
				return acceptingWriteCloser(), reader, reader.started
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, reader, ioStarted := test.newIO()
			waitCh := make(chan struct{})
			var closeWaitOnce sync.Once
			run := &rapidOCRWorkerRun{
				stdin:  writer,
				stdout: bufio.NewReader(reader),
				waitCh: waitCh,
			}
			run.terminateFn = func() {
				_ = writer.Close()
				_ = reader.Close()
				closeWaitOnce.Do(func() {
					close(waitCh)
				})
			}
			t.Cleanup(func() {
				run.terminate()
			})
			worker := NewRapidOCRWorker()
			worker.process.startProcess = func(string, string) (*rapidOCRWorkerRun, error) {
				return run, nil
			}
			deadlineCtx := newManualDeadlineContext()
			resultCh := make(chan error, 1)
			go func() {
				_, err := worker.Recognize(deadlineCtx, "unused.py", "image.png", "default")
				resultCh <- err
			}()
			waitForTestValue(t, ioStarted, "OCR I/O 阻塞")
			deadlineCtx.expire()
			if err := waitForTestValue(t, resultCh, "Recognize 响应 context"); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Recognize 错误 = %v，期望 context.DeadlineExceeded", err)
			}
			if err := worker.Shutdown(context.Background()); err != nil {
				t.Fatalf("清理 context 取消后的 OCR worker 失败: %v", err)
			}
		})
	}
}

func TestRapidOCRWorkerShutdownWaitsForChildProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestRapidOCRWorkerHelperProcess$")
	cmd.Env = append(os.Environ(), "SBM_OCR_WORKER_HELPER=1")
	run, err := startRapidOCRWorkerCommand(cmd)
	if err != nil {
		t.Fatalf("启动 OCR worker 测试子进程失败: %v", err)
	}
	t.Cleanup(func() {
		run.terminate()
		waitForTestValue(t, run.waitCh, "测试子进程回收")
	})
	worker := NewRapidOCRWorker()
	installRapidOCRWorkerRun(worker, run)

	ctx, cancel := context.WithTimeout(context.Background(), rapidOCRWorkerTestTimeout)
	defer cancel()
	if err := worker.Shutdown(ctx); err != nil {
		t.Fatalf("关闭 OCR worker 失败: %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("Shutdown 返回前必须等待子进程 Wait 完成")
	}
	worker.process.mu.Lock()
	defer worker.process.mu.Unlock()
	if worker.process.run != nil || worker.process.state != rapidOCRWorkerClosed {
		t.Fatal("Shutdown 后不应保留已回收的子进程句柄")
	}
}

func TestRapidOCRWorkerHelperProcess(t *testing.T) {
	if os.Getenv("SBM_OCR_WORKER_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}
