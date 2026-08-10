package services

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const rapidOCRWorkerShutdownTimeout = 5 * time.Second

// ErrOCRWorkerClosed 表示 Shutdown 已开始；关闭是终态，后续启动和识别都会返回该错误。
var ErrOCRWorkerClosed = errors.New("ocr worker is closing or closed")

type rapidOCRWorkerState uint8

// 状态机：idle -> starting -> running；失败或自动重启经 stopping -> idle；
// Shutdown 可从任意非终态进入 closing，并在唯一 Wait 完成后进入 closed。
const (
	rapidOCRWorkerIdle rapidOCRWorkerState = iota
	rapidOCRWorkerStarting
	rapidOCRWorkerRunning
	rapidOCRWorkerStopping
	rapidOCRWorkerClosing
	rapidOCRWorkerClosed
)

type rapidOCRWorkerRun struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	waitCh <-chan struct{}

	terminateOnce sync.Once
	terminateFn   func()
}

type rapidOCRWorkerProcess struct {
	initOnce    sync.Once
	requestGate chan struct{}
	closingCh   chan struct{}
	closeOnce   sync.Once

	mu        sync.Mutex
	state     rapidOCRWorkerState
	run       *rapidOCRWorkerRun
	startDone chan struct{}

	reqCount int64

	startProcess func(python string, scriptPath string) (*rapidOCRWorkerRun, error)
}

type OCRWorker interface {
	StartIfEnabled() (bool, error)
	Recognize(ctx context.Context, scriptPath string, imagePath string, profile string) ([]byte, error)
	Shutdown(ctx context.Context) error
}

type RapidOCRWorker struct {
	process rapidOCRWorkerProcess
}

func NewRapidOCRWorker() *RapidOCRWorker {
	return &RapidOCRWorker{}
}

func ocrWorkerEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_OCR_WORKER")))
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}

func getEnvInt64(key string, def int64) int64 {
	s := strings.TrimSpace(os.Getenv(key))
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func ocrWorkerRestartEveryN() int64 {
	// Default ON: restart periodically to prevent long-running Python + native libs from ballooning RSS.
	// Set to 0 to disable.
	n := getEnvInt64("SBM_OCR_WORKER_RESTART_EVERY_N", 50)
	if n < 0 {
		return 0
	}
	return n
}

func ocrWorkerMaxRSSBytes() int64 {
	// Optional: restart when worker RSS exceeds this threshold (MB). Disabled by default.
	mb := getEnvInt64("SBM_OCR_WORKER_MAX_RSS_MB", 0)
	if mb <= 0 {
		return 0
	}
	return mb * 1024 * 1024
}

func readLinuxRSSBytes(pid int) (int64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid")
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		// Example: "VmRSS:\t  12345 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("invalid VmRSS line")
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return kb * 1024, nil
	}
	return 0, fmt.Errorf("VmRSS not found")
}

func (w *rapidOCRWorkerProcess) shouldRestartLocked() (bool, string) {
	if w.state != rapidOCRWorkerRunning || w.run == nil || rapidOCRWorkerRunFinished(w.run) {
		return false, ""
	}

	if every := ocrWorkerRestartEveryN(); every > 0 && w.reqCount >= every {
		return true, fmt.Sprintf("request_count=%d threshold=%d", w.reqCount, every)
	}

	if maxRSS := ocrWorkerMaxRSSBytes(); maxRSS > 0 && runtime.GOOS == "linux" && w.run.cmd != nil && w.run.cmd.Process != nil {
		if rss, err := readLinuxRSSBytes(w.run.cmd.Process.Pid); err == nil && rss > maxRSS {
			return true, fmt.Sprintf("rss_bytes=%d threshold=%d", rss, maxRSS)
		}
	}

	return false, ""
}

func (w *RapidOCRWorker) StartIfEnabled() (bool, error) {
	if !ocrWorkerEnabled() {
		return false, nil
	}
	if w == nil {
		return false, fmt.Errorf("ocr worker is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), rapidOCRWorkerShutdownTimeout)
	defer cancel()
	if err := w.process.acquireRequest(ctx); err != nil {
		return false, err
	}
	defer w.process.releaseRequest()
	scriptPath := findOCRWorkerScript()
	if strings.TrimSpace(scriptPath) == "" {
		return false, fmt.Errorf("ocr worker enabled but scripts/ocr_worker.py not found")
	}
	if _, err := w.process.ensureStarted(ctx, scriptPath); err != nil {
		return false, err
	}
	return true, nil
}

func (w *RapidOCRWorker) Recognize(ctx context.Context, scriptPath string, imagePath string, profile string) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("ocr worker is nil")
	}
	ctx = nonNilContext(ctx)
	if err := w.process.acquireRequest(ctx); err != nil {
		return nil, err
	}
	defer w.process.releaseRequest()
	return w.process.recognize(ctx, scriptPath, imagePath, profile)
}

func (w *RapidOCRWorker) Shutdown(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return w.process.shutdown(ctx)
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func parseOCRProfileFromArgs(extraArgs []string) string {
	profile := "default"
	for i := 0; i < len(extraArgs); i++ {
		if strings.TrimSpace(extraArgs[i]) == "--profile" && i+1 < len(extraArgs) {
			p := strings.TrimSpace(extraArgs[i+1])
			if p != "" {
				profile = p
			}
			break
		}
	}
	if profile != "default" && profile != "pdf" {
		profile = "default"
	}
	return profile
}

func (s *OCRService) findOCRWorkerScript() string {
	return findOCRWorkerScript()
}

func findOCRWorkerScript() string {
	locations := []string{
		"scripts/ocr_worker.py",
		"../scripts/ocr_worker.py",
		"/app/scripts/ocr_worker.py",
		"./ocr_worker.py",
	}
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}
	return ""
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (w *rapidOCRWorkerProcess) init() {
	w.initOnce.Do(func() {
		w.requestGate = make(chan struct{}, 1)
		w.requestGate <- struct{}{}
		w.closingCh = make(chan struct{})
	})
}

func (w *rapidOCRWorkerProcess) acquireRequest(ctx context.Context) error {
	ctx = nonNilContext(ctx)
	w.init()
	if err := rapidOCRWorkerRequestInterruption(ctx, w.closingCh); err != nil {
		return err
	}
	select {
	case <-w.requestGate:
		if err := rapidOCRWorkerRequestInterruption(ctx, w.closingCh); err != nil {
			w.requestGate <- struct{}{}
			return err
		}
		return nil
	case <-w.closingCh:
		return ErrOCRWorkerClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *rapidOCRWorkerProcess) releaseRequest() {
	w.requestGate <- struct{}{}
}

func (r *rapidOCRWorkerRun) terminate() {
	if r == nil {
		return
	}
	r.terminateOnce.Do(func() {
		if r.terminateFn != nil {
			r.terminateFn()
		}
	})
}

func rapidOCRWorkerRunFinished(run *rapidOCRWorkerRun) bool {
	if run == nil || run.waitCh == nil {
		return true
	}
	select {
	case <-run.waitCh:
		return true
	default:
		return false
	}
}

func rapidOCRWorkerRequestInterruption(ctx context.Context, closingCh <-chan struct{}) error {
	ctx = nonNilContext(ctx)
	if closingCh != nil {
		select {
		case <-closingCh:
			return ErrOCRWorkerClosed
		default:
		}
	}
	return ctx.Err()
}

func waitForRapidOCRWorker(ctx context.Context, done <-chan struct{}, interrupt <-chan struct{}) error {
	ctx = nonNilContext(ctx)
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
	}
	if interrupt != nil {
		select {
		case <-interrupt:
			return ErrOCRWorkerClosed
		default:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	case <-interrupt:
		return ErrOCRWorkerClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// refreshLocked 只清理由唯一 Wait goroutine 已确认回收的进程句柄。
func (w *rapidOCRWorkerProcess) refreshLocked() {
	if w.run != nil && rapidOCRWorkerRunFinished(w.run) {
		w.run = nil
		w.reqCount = 0
	}
	if w.run != nil {
		return
	}
	switch w.state {
	case rapidOCRWorkerRunning, rapidOCRWorkerStopping:
		w.state = rapidOCRWorkerIdle
	case rapidOCRWorkerClosing:
		if w.startDone == nil {
			w.state = rapidOCRWorkerClosed
		}
	}
}

func (w *rapidOCRWorkerProcess) finishRun(run *rapidOCRWorkerRun) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.run != run {
		return
	}
	w.run = nil
	w.reqCount = 0
	if w.state == rapidOCRWorkerClosing {
		w.state = rapidOCRWorkerClosed
		return
	}
	w.state = rapidOCRWorkerIdle
}

func (w *rapidOCRWorkerProcess) markStopping(run *rapidOCRWorkerRun) {
	w.mu.Lock()
	if w.run == run && w.state == rapidOCRWorkerRunning {
		w.state = rapidOCRWorkerStopping
	}
	w.mu.Unlock()
	run.terminate()
}

func (w *rapidOCRWorkerProcess) shutdown(ctx context.Context) error {
	ctx = nonNilContext(ctx)
	w.init()
	w.closeOnce.Do(func() {
		close(w.closingCh)
	})

	for {
		w.mu.Lock()
		w.refreshLocked()
		if w.state == rapidOCRWorkerClosed {
			w.mu.Unlock()
			return nil
		}
		w.state = rapidOCRWorkerClosing
		if w.startDone != nil {
			startDone := w.startDone
			w.mu.Unlock()
			if err := waitForRapidOCRWorker(ctx, startDone, nil); err != nil {
				return fmt.Errorf("ocr worker shutdown waiting for start: %w", err)
			}
			continue
		}
		run := w.run
		if run == nil {
			w.state = rapidOCRWorkerClosed
			w.mu.Unlock()
			return nil
		}
		w.mu.Unlock()

		run.terminate()
		if err := waitForRapidOCRWorker(ctx, run.waitCh, nil); err != nil {
			return fmt.Errorf("ocr worker shutdown waiting for process: %w", err)
		}
		w.finishRun(run)
		return nil
	}
}

func startRapidOCRWorkerProcess(python string, scriptPath string) (*rapidOCRWorkerRun, error) {
	cmd := exec.Command(python, scriptPath)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	return startRapidOCRWorkerCommand(cmd)
}

func startRapidOCRWorkerCommand(cmd *exec.Cmd) (*rapidOCRWorkerRun, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdoutPipe.Close()
		return nil, err
	}
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return nil, err
	}

	// 持续排空 stderr，避免子进程因日志管道写满而阻塞。
	go func() {
		_, _ = io.Copy(io.Discard, stderrPipe)
	}()

	waitCh := make(chan struct{})
	run := &rapidOCRWorkerRun{
		cmd:    cmd,
		stdin:  stdinPipe,
		stdout: bufio.NewReaderSize(stdoutPipe, 1024*1024),
		waitCh: waitCh,
	}
	run.terminateFn = func() {
		_ = stdinPipe.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = stdoutPipe.Close()
	}
	go func() {
		_ = cmd.Wait()
		close(waitCh)
	}()
	return run, nil
}

func (w *rapidOCRWorkerProcess) startNew(ctx context.Context, scriptPath string) (*rapidOCRWorkerRun, error) {
	starter := w.startProcess
	if starter == nil {
		starter = startRapidOCRWorkerProcess
	}
	var lastErr error
	for _, python := range []string{"python3", "python"} {
		if err := rapidOCRWorkerRequestInterruption(ctx, w.closingCh); err != nil {
			return nil, err
		}
		run, err := starter(python, scriptPath)
		if err != nil {
			lastErr = err
			continue
		}
		if run == nil || run.stdin == nil || run.stdout == nil || run.waitCh == nil {
			if run != nil {
				run.terminate()
			}
			lastErr = fmt.Errorf("ocr worker starter returned incomplete process handles")
			continue
		}
		return run, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("failed to start ocr worker")
	}
	return nil, lastErr
}

// ensureStarted 在请求闸门内调用；进程启动和 Wait 均不持有生命周期锁。
func (w *rapidOCRWorkerProcess) ensureStarted(ctx context.Context, scriptPath string) (*rapidOCRWorkerRun, error) {
	ctx = nonNilContext(ctx)
	w.init()
	for {
		w.mu.Lock()
		w.refreshLocked()
		if err := rapidOCRWorkerRequestInterruption(ctx, w.closingCh); err != nil {
			w.mu.Unlock()
			return nil, err
		}
		switch w.state {
		case rapidOCRWorkerClosing, rapidOCRWorkerClosed:
			w.mu.Unlock()
			return nil, ErrOCRWorkerClosed
		case rapidOCRWorkerStarting:
			startDone := w.startDone
			w.mu.Unlock()
			if err := waitForRapidOCRWorker(ctx, startDone, w.closingCh); err != nil {
				return nil, err
			}
			continue
		case rapidOCRWorkerRunning:
			run := w.run
			shouldRestart, reason := w.shouldRestartLocked()
			if !shouldRestart {
				w.mu.Unlock()
				return run, nil
			}
			w.state = rapidOCRWorkerStopping
			w.mu.Unlock()
			log.Printf("[OCRWorker] restarting python worker (%s)", reason)
			run.terminate()
			if err := waitForRapidOCRWorker(ctx, run.waitCh, w.closingCh); err != nil {
				return nil, fmt.Errorf("ocr worker restart waiting for process: %w", err)
			}
			w.finishRun(run)
			continue
		case rapidOCRWorkerStopping:
			run := w.run
			w.mu.Unlock()
			run.terminate()
			if err := waitForRapidOCRWorker(ctx, run.waitCh, w.closingCh); err != nil {
				return nil, fmt.Errorf("ocr worker recovery waiting for process: %w", err)
			}
			w.finishRun(run)
			continue
		case rapidOCRWorkerIdle:
			startDone := make(chan struct{})
			w.state = rapidOCRWorkerStarting
			w.startDone = startDone
			w.mu.Unlock()

			run, startErr := w.startNew(ctx, scriptPath)
			interruption := rapidOCRWorkerRequestInterruption(ctx, w.closingCh)

			w.mu.Lock()
			terminal := errors.Is(interruption, ErrOCRWorkerClosed) || w.state == rapidOCRWorkerClosing || w.state == rapidOCRWorkerClosed
			if run != nil {
				w.run = run
			}
			switch {
			case terminal:
				w.state = rapidOCRWorkerClosing
			case interruption != nil && run != nil:
				w.state = rapidOCRWorkerStopping
			case startErr != nil:
				w.state = rapidOCRWorkerIdle
			default:
				w.state = rapidOCRWorkerRunning
				w.reqCount = 0
			}
			w.startDone = nil
			close(startDone)
			w.mu.Unlock()

			if terminal {
				if run != nil {
					run.terminate()
				}
				return nil, ErrOCRWorkerClosed
			}
			if interruption != nil {
				if run != nil {
					run.terminate()
				}
				return nil, fmt.Errorf("ocr worker start canceled: %w", interruption)
			}
			if startErr != nil {
				return nil, startErr
			}
			return run, nil
		default:
			invalidState := w.state
			w.mu.Unlock()
			return nil, fmt.Errorf("ocr worker has invalid lifecycle state %d", invalidState)
		}
	}
}

type ocrWorkerBaseResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func writeRapidOCRWorkerRequest(ctx context.Context, closingCh <-chan struct{}, run *rapidOCRWorkerRun, payload []byte) error {
	if err := rapidOCRWorkerRequestInterruption(ctx, closingCh); err != nil {
		return err
	}
	resultCh := make(chan error, 1)
	go func() {
		n, err := run.stdin.Write(payload)
		if err == nil && n != len(payload) {
			err = io.ErrShortWrite
		}
		resultCh <- err
	}()
	select {
	case err := <-resultCh:
		if interruption := rapidOCRWorkerRequestInterruption(ctx, closingCh); interruption != nil {
			return interruption
		}
		return err
	case <-closingCh:
		return ErrOCRWorkerClosed
	case <-ctx.Done():
		return rapidOCRWorkerRequestInterruption(ctx, closingCh)
	}
}

type rapidOCRWorkerReadResult struct {
	line []byte
	err  error
}

func readRapidOCRWorkerResponse(ctx context.Context, closingCh <-chan struct{}, run *rapidOCRWorkerRun) ([]byte, error) {
	if err := rapidOCRWorkerRequestInterruption(ctx, closingCh); err != nil {
		return nil, err
	}
	resultCh := make(chan rapidOCRWorkerReadResult, 1)
	go func() {
		line, err := run.stdout.ReadBytes('\n')
		resultCh <- rapidOCRWorkerReadResult{line: line, err: err}
	}()
	select {
	case result := <-resultCh:
		if interruption := rapidOCRWorkerRequestInterruption(ctx, closingCh); interruption != nil {
			return nil, interruption
		}
		return result.line, result.err
	case <-closingCh:
		return nil, ErrOCRWorkerClosed
	case <-ctx.Done():
		return nil, rapidOCRWorkerRequestInterruption(ctx, closingCh)
	}
}

func rapidOCRWorkerRequestError(operation string, err error) error {
	switch {
	case errors.Is(err, ErrOCRWorkerClosed):
		return fmt.Errorf("ocr worker request interrupted: %w", ErrOCRWorkerClosed)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("ocr worker request canceled during %s: %w", operation, err)
	default:
		return fmt.Errorf("ocr worker %s failed: %w", operation, err)
	}
}

func (w *rapidOCRWorkerProcess) recognize(ctx context.Context, scriptPath string, imagePath string, profile string) ([]byte, error) {
	run, err := w.ensureStarted(ctx, scriptPath)
	if err != nil {
		return nil, err
	}

	w.mu.Lock()
	w.refreshLocked()
	interruption := rapidOCRWorkerRequestInterruption(ctx, w.closingCh)
	if interruption != nil || w.state != rapidOCRWorkerRunning || w.run != run {
		w.mu.Unlock()
		if interruption != nil {
			w.markStopping(run)
			return nil, rapidOCRWorkerRequestError("start", interruption)
		}
		return nil, fmt.Errorf("ocr worker process exited before request")
	}
	w.reqCount++
	w.mu.Unlock()

	reqID := randHex(12)
	req := map[string]any{
		"id":         reqID,
		"type":       "ocr",
		"image_path": imagePath,
		"profile":    profile,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal ocr worker request: %w", err)
	}
	b = append(b, '\n')

	if err := writeRapidOCRWorkerRequest(ctx, w.closingCh, run, b); err != nil {
		w.markStopping(run)
		return nil, rapidOCRWorkerRequestError("write", err)
	}

	response, err := readRapidOCRWorkerResponse(ctx, w.closingCh, run)
	if err != nil {
		w.markStopping(run)
		return nil, rapidOCRWorkerRequestError("read", err)
	}
	line := bytes.TrimSpace(response)
	if len(line) == 0 {
		w.markStopping(run)
		return nil, fmt.Errorf("ocr worker returned empty response")
	}

	var base ocrWorkerBaseResponse
	if err := unmarshalPossiblyNoisyJSON(line, &base); err != nil {
		w.markStopping(run)
		return nil, fmt.Errorf("ocr worker returned invalid json: %w", err)
	}
	if strings.TrimSpace(base.ID) != reqID {
		w.markStopping(run)
		return nil, fmt.Errorf("ocr worker response id mismatch")
	}
	if !base.Success {
		if strings.TrimSpace(base.Error) == "" {
			return nil, fmt.Errorf("ocr worker failed")
		}
		return nil, fmt.Errorf("ocr worker failed: %s", strings.TrimSpace(base.Error))
	}
	if err := rapidOCRWorkerRequestInterruption(ctx, w.closingCh); err != nil {
		w.markStopping(run)
		return nil, rapidOCRWorkerRequestError("read", err)
	}
	return line, nil
}
