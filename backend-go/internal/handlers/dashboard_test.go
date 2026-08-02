package handlers

import (
	"testing"
	"time"
)

func TestDashboardMonthBoundsUsesShanghaiCalendar(t *testing.T) {
	now := time.Date(2024, 2, 15, 12, 0, 0, 0, time.UTC)
	start, end := dashboardMonthBounds(now)

	if got := start.Format(time.RFC3339Nano); got != "2024-02-01T00:00:00+08:00" {
		t.Fatalf("月初边界错误: %s", got)
	}
	if got := end.Format(time.RFC3339Nano); got != "2024-02-29T23:59:59.999+08:00" {
		t.Fatalf("月末边界错误: %s", got)
	}
}
