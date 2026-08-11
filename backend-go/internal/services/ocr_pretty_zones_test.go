package services

import (
	"strings"
	"testing"
)

func TestFixInvoiceZonesForPretty_MergesFakePasswordZoneIntoBuyer(t *testing.T) {
	// Simulate a template without password area: fixed-region split still emits "【密码区】",
	// and buyer/table text leaks into it. The pretty fixer should merge/remove it.
	in := []string{
		"SYNTHETIC / 纯合成测试数据",
		"【第1页-分区】",
		"【发票信息】",
		"发票号码：99000000000000000301",
		"【购买方】",
		"购买方信息名称：个人",
		"项目名称 规格型号 单位 数量",
		"【密码区】",
		"购买方信息名称：个人",
		"地址、电话：纯合成地址",
		"【明细】",
		"*纯合成服务*测试预订服务 个 1",
	}

	out := fixInvoiceZonesForPretty(in, nil)
	pretty := strings.Join(out, "\n")
	if strings.Contains(pretty, "【密码区】") {
		t.Fatalf("expected fake 【密码区】 removed/merged, got:\n%s", pretty)
	}
	if !strings.Contains(pretty, "【购买方】") || !strings.Contains(pretty, "购买方信息名称：个人") {
		t.Fatalf("expected buyer zone to include merged content, got:\n%s", pretty)
	}
}

func TestNormalizeInvoiceTextForPretty_KeepRealPasswordZone(t *testing.T) {
	raw := syntheticOCRText(
		"【第1页-分区】",
		"【购买方】",
		"名称: 纯合成购买方甲",
		"【密码区】",
		"密 码 区 42.00 *99<<SYNTHETIC>>++",
		"【明细】",
		"*纯合成服务*测试充值 元 1",
	)

	pretty := normalizeInvoiceTextForPretty(raw, nil)
	if !strings.Contains(pretty, "【密码区】") {
		t.Fatalf("expected password zone preserved, got:\n%s", pretty)
	}
}

func TestFixInvoiceZonesForPretty_DistributesMergedLinesToSellerAndDetail(t *testing.T) {
	in := []string{
		"SYNTHETIC / 纯合成测试数据",
		"【第1页-分区】",
		"【发票信息】",
		"发票号码：99000000000000000302",
		"【购买方】",
		"购买方信息名称：个人",
		"【密码区】",
		"项目名称 规格型号 单位 数量",
		"统一社会信用代码/纳税人识别号: 纯合成旅游服务有限公司 91110000SYNTH00001",
		"单价 42.00 金额 42.00 税率/征收率 6% 税额 2.52",
		"【明细】",
		"*纯合成服务*测试预订服务 个 1",
		"【销售方】",
		"备注",
	}

	data := &InvoiceExtractedData{
		BuyerName:  ptrString("个人"),
		SellerName: ptrString("纯合成旅游服务有限公司"),
	}

	out := fixInvoiceZonesForPretty(in, data)
	pretty := strings.Join(out, "\n")
	if strings.Contains(pretty, "【密码区】") {
		t.Fatalf("expected fake 【密码区】 removed/merged, got:\n%s", pretty)
	}

	sellerIdx := strings.Index(pretty, "【销售方】")
	if sellerIdx == -1 || !strings.Contains(pretty[sellerIdx:], "纯合成旅游服务有限公司") {
		t.Fatalf("expected seller zone to include company line, got:\n%s", pretty)
	}
	if strings.Contains(pretty[sellerIdx:], "税率/征收率") {
		t.Fatalf("expected tax columns to be moved to 【明细】, got seller zone:\n%s", pretty[sellerIdx:])
	}

	detailIdx := strings.Index(pretty, "【明细】")
	if detailIdx == -1 || !strings.Contains(pretty[detailIdx:], "税率/征收率") {
		t.Fatalf("expected detail zone to include tax columns, got:\n%s", pretty)
	}
}
