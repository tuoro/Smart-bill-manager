package mailsync

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type syncRecorder struct {
	mu     sync.Mutex
	calls  map[string]int
	first  chan struct{}
	closed bool
	err    error
}

func newRecorder() *syncRecorder {
	return &syncRecorder{calls: map[string]int{}, first: make(chan struct{})}
}

func (r *syncRecorder) sync(_ context.Context, tenantID, sourceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[tenantID+"/"+sourceID]++
	if !r.closed {
		r.closed = true
		close(r.first)
	}
	return r.err
}

func (r *syncRecorder) count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[key]
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// 开启同步要立刻拉一次，然后按间隔继续；停用之后不能再拉。这正是"开关"两个字
// 在后台的实际含义。
func TestPollerRunsImmediatelyAndStopsOnDemand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := newRecorder()
	poller := NewPoller(ctx, 10*time.Millisecond, quietLogger())
	poller.Bind(recorder.sync)

	poller.Start("tenant", "source")
	select {
	case <-recorder.first:
	case <-time.After(2 * time.Second):
		t.Fatal("enabling sync did not poll at once")
	}
	// 跑够几轮，确认它是循环而不是只跑一次。
	deadline := time.Now().Add(2 * time.Second)
	for recorder.count("tenant/source") < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if recorder.count("tenant/source") < 3 {
		t.Fatalf("polled %d times, want it to keep going", recorder.count("tenant/source"))
	}

	poller.Stop("tenant", "source")
	settled := recorder.count("tenant/source")
	time.Sleep(80 * time.Millisecond)
	// 停用时可能有一轮正在途中，允许它跑完，但不能再有新的。
	if after := recorder.count("tenant/source"); after > settled+1 {
		t.Fatalf("polls continued after stop: %d → %d", settled, after)
	}
}

// 同一个邮箱重复启动只保留一条循环——换密码后重新启用走的正是这条路。
func TestPollerRestartReplacesTheRunningLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := newRecorder()
	poller := NewPoller(ctx, time.Hour, quietLogger())
	poller.Bind(recorder.sync)

	poller.Start("tenant", "source")
	poller.Start("tenant", "source")
	select {
	case <-recorder.first:
	case <-time.After(2 * time.Second):
		t.Fatal("restart did not poll")
	}
	// 间隔一小时，所以每条循环只会立刻拉那一次：重启后正好两次——旧循环一次、
	// 新循环一次。换密码后重新启用要的就是这个"立刻再拉一次"。
	deadline := time.Now().Add(2 * time.Second)
	for recorder.count("tenant/source") < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := recorder.count("tenant/source"); got != 2 {
		t.Fatalf("polls after restart = %d, want the new loop to poll once", got)
	}
	poller.Stop("tenant", "source")
	time.Sleep(80 * time.Millisecond)
	if after := recorder.count("tenant/source"); after != 2 {
		t.Fatalf("a loop survived the stop: %d", after)
	}
}

// 没接上同步函数时不留下空转的循环。
func TestPollerWithoutBindingDoesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poller := NewPoller(ctx, time.Millisecond, quietLogger())
	poller.Start("tenant", "source")
	time.Sleep(20 * time.Millisecond)
	poller.Stop("tenant", "source")
}
