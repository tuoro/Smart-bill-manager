package services

import (
	"strings"
	"testing"
)

func TestNormalizeInvoiceTextForPretty_MergesNonPasswordPasswordZoneIntoBuyer(t *testing.T) {
	// This test targets the zoned pretty-text fix (not the general spacing normalizer).
	in := []string{
		"【第1页-分区】",
		"【发票信息】",
		"发票号码： 26117000000093487418",
		"【购买方】",
		"购买方信息名称： 个人",
		"【密码区】",
		"统一社会信用代码/纳税人识别号: 北京易行出行旅游有限公司91110108735575307R",
		"单价65.42金额65.42税率/征收率6% 税额3.92",
		"【明细】",
		"*旅游服务*代订车服务费 个 1",
	}
	block := strings.Join(in, "\n")
	if !strings.Contains(block, "纳税人识别号") || !strings.Contains(block, "统一社会信用代码") {
		t.Fatalf("test setup invalid: block should contain buyer/seller markers, got:\n%s", block)
	}
	if in[5] != "【密码区】" {
		t.Fatalf("test setup invalid: expected header literal match, got=%q", in[5])
	}
	out := fixInvoiceZonesForPretty(in)
	pretty := strings.Join(out, "\n")
	if strings.Contains(pretty, "【密码区】") {
		t.Fatalf("expected password zone removed/merged, got:\n%s", pretty)
	}
	if !strings.Contains(pretty, "【购买方】") || !strings.Contains(pretty, "北京易行出行旅游有限公司") {
		t.Fatalf("expected buyer zone to include merged content, got:\n%s", pretty)
	}
}

func TestNormalizeInvoiceTextForPretty_KeepRealPasswordZone(t *testing.T) {
	raw := strings.Join([]string{
		"【第1页-分区】",
		"【购买方】",
		"名称: 武亚峰",
		"【密码区】",
		"密 码 区 200.00 *14<<*>07/6>27/*88780<>*>45",
		"【明细】",
		"*电信服务*话费充值 元 1",
	}, "\n")

	pretty := normalizeInvoiceTextForPretty(raw)
	if !strings.Contains(pretty, "【密码区】") {
		t.Fatalf("expected password zone preserved, got:\n%s", pretty)
	}
}
