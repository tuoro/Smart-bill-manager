package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"smart-bill-manager/internal/utils"
)

type rateLimiter struct {
	requests map[string][]time.Time
	mu       sync.RWMutex
	window   time.Duration
	max      int
}

func newRateLimiter(window time.Duration, max int) *rateLimiter {
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		window:   window,
		max:      max,
	}
	
	// Clean up old entries periodically
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			rl.cleanup()
		}
	}()
	
	return rl
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	cutoff := time.Now().Add(-rl.window)
	for ip, times := range rl.requests {
		var valid []time.Time
		for _, t := range times {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.requests, ip)
		} else {
			rl.requests[ip] = valid
		}
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	cutoff := now.Add(-rl.window)
	
	// Filter old requests
	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	
	if len(valid) >= rl.max {
		return false
	}
	
	valid = append(valid, now)
	rl.requests[ip] = valid
	return true
}

// AuthRateLimitMiddleware creates rate limiter for auth endpoints (stricter)
func AuthRateLimitMiddleware() gin.HandlerFunc {
	limiter := newRateLimiter(15*time.Minute, 20) // 20 requests per 15 minutes
	
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.allow(ip) {
			utils.Error(c, 429, "请求过于频繁，请稍后再试", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

// APIRateLimitMiddleware creates rate limiter for general API
func APIRateLimitMiddleware() gin.HandlerFunc {
	limiter := newRateLimiter(time.Minute, 100) // 100 requests per minute
	
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.allow(ip) {
			utils.Error(c, 429, "请求过于频繁，请稍后再试", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}
