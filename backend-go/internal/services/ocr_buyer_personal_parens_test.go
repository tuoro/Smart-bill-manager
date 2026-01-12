package services

import "testing"

func TestParseInvoiceData_BuyerPersonalWithParens_NotTruncated(t *testing.T) {
	svc := NewOCRService()

	raw := `【第1页-分区】
【发票信息】
发票号码： 25312000000427429429
开票日期： 2025年12月23日
电子发票（普通发票）
【购买方】
购买方信息统一社会信用代码/纳税人识别号： 名称： 个人（个人）
项目名称规格型号单位数量
【密码区】
上海道道鲜餐饮管理有限公司
统一社会信用代码/纳税人识别号： 91310230664388466W
2483.02单价2483.02金额税率/征收率6% 税额148.98
`

	got, err := svc.ParseInvoiceData(raw)
	if err != nil {
		t.Fatalf("ParseInvoiceData error: %v", err)
	}
	if got.BuyerName == nil || *got.BuyerName != "个人（个人）" {
		t.Fatalf("expected buyer=%q got %+v (src=%q conf=%v)", "个人（个人）", got.BuyerName, got.BuyerNameSource, got.BuyerNameConfidence)
	}
}

