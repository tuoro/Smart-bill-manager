package services

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type limiter struct {
	ch chan struct{}
}

func newLimiter(size int) *limiter {
	if size <= 0 {
		return &limiter{ch: nil}
	}
	return &limiter{ch: make(chan struct{}, size)}
}

func (l *limiter) acquire(ctx context.Context) (func(), error) {
	if l == nil || l.ch == nil {
		return func() {}, nil
	}
	select {
	case l.ch <- struct{}{}:
		return func() { <-l.ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func envIntDefault(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

var (
	limitOCR           = newLimiter(envIntDefault("SBM_LIMIT_OCR", 2))
	limitZipExport     = newLimiter(envIntDefault("SBM_LIMIT_EXPORT", 1))
	limitEmailDownload = newLimiter(envIntDefault("SBM_LIMIT_EMAIL_DOWNLOAD", 2))
)

func acquireWithTimeout(parent context.Context, l *limiter, timeout time.Duration, label string) (func(), error) {
	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	release, err := l.acquire(ctx)
	cancel()
	if err != nil {
		if label == "" {
			label = "operation"
		}
		return nil, fmt.Errorf("%s busy: %w", label, err)
	}
	return release, nil
}

func AcquireZipExport(ctx context.Context) (func(), error) {
	return acquireWithTimeout(ctx, limitZipExport, 5*time.Second, "zip export")
}

func AcquireEmailDownload(ctx context.Context) (func(), error) {
	return acquireWithTimeout(ctx, limitEmailDownload, 10*time.Second, "email download")
}
