package handlers

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/utils"
)

const defaultReadTimeout = 10 * time.Second

func readTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("SBM_API_READ_TIMEOUT_MS"))
	if v == "" {
		return defaultReadTimeout
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultReadTimeout
	}
	return time.Duration(n) * time.Millisecond
}

func withReadTimeout(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), readTimeout())
}

func handleReadTimeoutError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		utils.Error(c, 504, "request timeout", err)
		return true
	}
	if errors.Is(err, context.Canceled) {
		utils.Error(c, 408, "request canceled", err)
		return true
	}
	return false
}
