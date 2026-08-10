package services

import "testing"

func TestStripTrailingMoneyTokensFromItemField(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"*纯合成服务*测试路程费42.36", "*纯合成服务*测试路程费"},
		{"*纯合成服务*测试路程费 42.36", "*纯合成服务*测试路程费"},
		{"iPhone 15.00", "iPhone 15.00"}, // do not strip non-Chinese item names blindly
	}

	for _, c := range cases {
		got := stripTrailingMoneyTokensFromItemField(c.in)
		if got != c.want {
			t.Fatalf("stripTrailingMoneyTokensFromItemField(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
