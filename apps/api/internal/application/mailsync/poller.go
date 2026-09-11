package mailsync

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Poller 实现 Runtime：每个开着同步的邮箱一个循环，启动即跑一轮，之后按间隔轮询。
// 它自己不碰网络，只是定时调用 Service.SyncOnce。
type Poller struct {
	ctx      context.Context
	interval time.Duration
	logger   *slog.Logger
	sync     func(ctx context.Context, tenantID, sourceID string) error

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func NewPoller(ctx context.Context, interval time.Duration, logger *slog.Logger) *Poller {
	return &Poller{ctx: ctx, interval: interval, logger: logger, running: map[string]context.CancelFunc{}}
}

// Bind 在 Service 构造好之后接上同步函数（两者互相引用，只能后绑）。
func (p *Poller) Bind(sync func(ctx context.Context, tenantID, sourceID string) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sync = sync
}

func key(tenantID, sourceID string) string { return tenantID + ":" + sourceID }

func (p *Poller) Start(tenantID, sourceID string) {
	p.Stop(tenantID, sourceID)
	ctx, cancel := context.WithCancel(p.ctx)
	p.mu.Lock()
	p.running[key(tenantID, sourceID)] = cancel
	run := p.sync
	p.mu.Unlock()
	if run == nil {
		cancel()
		return
	}
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			if err := run(ctx, tenantID, sourceID); err != nil && ctx.Err() == nil {
				p.logger.Warn("mailbox sync failed", "tenant", tenantID, "source", sourceID, "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (p *Poller) Stop(tenantID, sourceID string) {
	p.mu.Lock()
	cancel, ok := p.running[key(tenantID, sourceID)]
	if ok {
		delete(p.running, key(tenantID, sourceID))
	}
	p.mu.Unlock()
	if ok {
		cancel()
	}
}
