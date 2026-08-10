package services

import (
	"strings"
	"testing"
)

func TestNormalizeInvoiceTextForPretty_KeepPasswordZone_WhenItLooksLegit(t *testing.T) {
	// 纯合成分区模拟：密码区包含明确的销售方与税号字段，应原样保留。
	raw := syntheticOCRText(
		"【第1页-分区】",
		"【发票信息】",
		"发票号码：99000000000000000101",
		"开票日期：2026年08月01日",
		"【购买方】",
		"购买方信息名称：纯合成购买方甲",
		"【密码区】",
		"<<SYNTHETIC***+++>>>",
		"销售方信息名称：纯合成销售方有限公司",
		"纳税人识别号：SYNTHETIC-TAX-0001",
		"【明细】",
		"*纯合成服务*测试服务 标准 次 1",
	)
	buyer := "纯合成购买方甲"
	seller := "纯合成销售方有限公司"
	data := &InvoiceExtractedData{BuyerName: &buyer, SellerName: &seller}

	clean := normalizeInvoiceTextForPretty(raw, data)
	if !strings.Contains(clean, "【密码区】") {
		t.Fatalf("expected pretty text to keep 【密码区】, got:\n%s", clean)
	}
}

func TestNormalizeInvoiceTextForPretty_MergePasswordZone_WhenBuyerLeaksIntoIt(t *testing.T) {
	// Synthetic "no password area" layout: fixed-region split emits "【密码区】" but the content is clearly buyer/table info.
	raw := syntheticOCRText(
		"【第1页-分区】",
		"【发票信息】",
		"发票号码： 123",
		"开票日期： 2026年01月01日",
		"【购买方】",
		"购买方信息名称： 纯合成购买方乙",
		"项目名称 规格型号 单位 数量",
		"【密码区】",
		"购买方信息名称： 纯合成购买方乙",
		"地址、电话： 纯合成地址",
		"【明细】",
		"*纯合成服务*测试服务 个 1",
		"【销售方】",
		"销售方信息名称： 纯合成销售方有限公司",
	)

	buyer := "纯合成购买方乙"
	seller := "纯合成销售方有限公司"
	data := &InvoiceExtractedData{
		BuyerName:  &buyer,
		SellerName: &seller,
	}

	clean := normalizeInvoiceTextForPretty(raw, data)
	if strings.Contains(clean, "【密码区】") {
		t.Fatalf("expected pretty text to merge/remove fake 【密码区】, got:\n%s", clean)
	}
	compact := strings.ReplaceAll(strings.ReplaceAll(clean, " ", ""), "\t", "")
	if !strings.Contains(compact, "【购买方】") || !strings.Contains(compact, "购买方信息名称：纯合成购买方乙") {
		t.Fatalf("expected buyer info to be present in pretty text, got:\n%s", clean)
	}
}
