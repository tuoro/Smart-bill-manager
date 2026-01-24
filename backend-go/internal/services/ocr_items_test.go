package services

import "testing"

func TestStripTrailingMoneyTokensFromItemField(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"*运输服务*客运服务费68.34", "*运输服务*客运服务费"},
		{"*运输服务*客运服务费 68.34", "*运输服务*客运服务费"},
		{"iPhone 15.00", "iPhone 15.00"}, // do not strip non-Chinese item names blindly
	}

	for _, c := range cases {
		got := stripTrailingMoneyTokensFromItemField(c.in)
		if got != c.want {
			t.Fatalf("stripTrailingMoneyTokensFromItemField(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

