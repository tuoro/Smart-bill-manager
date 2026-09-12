package chatdialogue

import (
	"encoding/json"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func snapshotWith(currency, timezone string) ports.ReviewSnapshot {
	return ports.ReviewSnapshot{Fields: []ports.ReviewField{
		{Path: "currency", ValueType: "string", Presence: "present", Value: json.RawMessage(`"` + currency + `"`)},
		{Path: "source_timezone", ValueType: "string", Presence: "present", Value: json.RawMessage(`"` + timezone + `"`)},
	}}
}

// 聊天里输入的每种字段都有固定写法。写错时要说清该怎么写，而不是猜一个值存进去。
func TestParseFieldValueAcceptsOnlyExactWritings(t *testing.T) {
	t.Parallel()
	review := snapshotWith("CNY", "Asia/Shanghai")
	cases := []struct {
		name      string
		valueType string
		input     string
		value     string
		display   string
		rejected  bool
	}{
		{name: "字符串", valueType: "string", input: " 星巴克 ", value: `"星巴克"`, display: "星巴克"},
		{name: "金额", valueType: "money_minor", input: "12.34", value: "1234", display: "CNY 12.34"},
		{name: "金额小数位超精度", valueType: "money_minor", input: "12.345", rejected: true},
		{name: "金额不是数字", valueType: "money_minor", input: "十二块", rejected: true},
		{name: "时刻", valueType: "instant", input: "2026-09-10 12:30",
			value: `"2026-09-10T12:30:00+08:00"`, display: "2026-09-10 12:30  Asia/Shanghai"},
		{name: "时刻缺分钟", valueType: "instant", input: "2026-09-10 12", rejected: true},
		{name: "时刻用斜杠", valueType: "instant", input: "9/10 12:30", rejected: true},
		{name: "日期", valueType: "date", input: "2026-09-10", value: `"2026-09-10"`, display: "2026-09-10"},
		{name: "日期写反", valueType: "date", input: "10-09-2026", rejected: true},
		{name: "整数", valueType: "integer", input: "7", value: "7", display: "7"},
		{name: "整数带小数", valueType: "integer", input: "7.5", rejected: true},
		{name: "十进制", valueType: "decimal", input: "1.500", value: `"1.500"`, display: "1.500"},
		{name: "空值", valueType: "string", input: "   ", rejected: true},
		{name: "结构化字段不许在聊天里改", valueType: "supplementary", input: "任意", rejected: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			field := ports.ReviewField{ValueType: testCase.valueType}
			value, display, err := parseFieldValue(review, field, testCase.input)
			if testCase.rejected {
				if err == nil {
					t.Fatalf("accepted %q as %s", testCase.input, testCase.valueType)
				}
				if err.Error() == "" {
					t.Fatal("rejection must explain how to write it")
				}
				return
			}
			if err != nil {
				t.Fatalf("rejected %q: %v", testCase.input, err)
			}
			if string(value) != testCase.value || display != testCase.display {
				t.Fatalf("value = %s / %q, want %s / %q", value, display, testCase.value, testCase.display)
			}
		})
	}
}

// 没有时区的单据不能在聊天里改时间：换算不出确定的时刻，就不换算。
func TestInstantWithoutTimezoneIsRefused(t *testing.T) {
	t.Parallel()
	review := snapshotWith("CNY", "")
	if _, _, err := parseFieldValue(review, ports.ReviewField{ValueType: "instant"}, "2026-09-10 12:30"); err == nil {
		t.Fatal("instant accepted without a source timezone")
	}
}

func TestParseFieldNumbers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		limit int
		want  []int
		ok    bool
	}{
		{input: "3", limit: 7, want: []int{2}, ok: true},
		{input: "1,3", limit: 3, want: []int{0, 2}, ok: true},
		{input: "1，3", limit: 3, want: []int{0, 2}, ok: true},
		{input: " 2 、 1 ", limit: 3, want: []int{1, 0}, ok: true},
		{input: "1,1", limit: 3},
		{input: "0", limit: 3},
		{input: "4", limit: 3},
		{input: "", limit: 3},
		{input: "确认", limit: 3},
	}
	for _, testCase := range cases {
		got, ok := parseFieldNumbers(testCase.input, testCase.limit)
		if ok != testCase.ok {
			t.Fatalf("%q ok = %v, want %v", testCase.input, ok, testCase.ok)
		}
		if !ok {
			continue
		}
		if len(got) != len(testCase.want) {
			t.Fatalf("%q = %v, want %v", testCase.input, got, testCase.want)
		}
		for index := range got {
			if got[index] != testCase.want[index] {
				t.Fatalf("%q = %v, want %v", testCase.input, got, testCase.want)
			}
		}
	}
}
