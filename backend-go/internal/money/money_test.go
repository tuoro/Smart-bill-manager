package money

import (
	"errors"
	"math"
	"testing"
)

func TestFromMajor(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  int64
	}{
		{name: "整数", value: 12, want: 1200},
		{name: "两位小数", value: 12.34, want: 1234},
		{name: "浮点运算误差", value: 0.1 + 0.2, want: 30},
		{name: "十进制中点", value: 1.005, want: 101},
		{name: "负十进制中点", value: -1.005, want: -101},
		{name: "第三位四舍五入", value: 1.235, want: 124},
		{name: "负数", value: -8.765, want: -877},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := FromMajor(test.value)
			if err != nil {
				t.Fatalf("转换失败: %v", err)
			}
			if got != test.want {
				t.Fatalf("FromMajor(%v)=%d，期望 %d", test.value, got, test.want)
			}
		})
	}
}

func TestFromMajorRejectsInvalidValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), float64(maxSafeCents)} {
		if _, err := FromMajor(value); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("应拒绝金额 %v，实际错误为 %v", value, err)
		}
	}
}

func TestSyncUpdateMap(t *testing.T) {
	data := map[string]any{"amount": 12.345}
	if err := SyncUpdateMap(data, "amount", "amount_cents", false); err != nil {
		t.Fatalf("同步金额失败: %v", err)
	}
	if data["amount"] != 12.35 || data["amount_cents"] != int64(1235) {
		t.Fatalf("金额未正确同步: %#v", data)
	}

	nullable := map[string]any{"amount": nil}
	if err := SyncUpdateMap(nullable, "amount", "amount_cents", true); err != nil {
		t.Fatalf("同步空金额失败: %v", err)
	}
	if value, exists := nullable["amount_cents"]; !exists || value != nil {
		t.Fatalf("空金额未同步到分字段: %#v", nullable)
	}
}
