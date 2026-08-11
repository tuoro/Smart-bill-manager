package services

import "testing"

func TestExtractCompanyNameNearTaxID_PicksLongestNestedMatch(t *testing.T) {
	text := syntheticOCRText("统一社会信用代码/纳税人识别号: 纯合成长名称旅游服务有限公司91110000SYNTH00001单价42.00金额42.00税率/征收率6% 税额2.52")
	got := extractCompanyNameNearTaxID(text)
	if got != "纯合成长名称旅游服务有限公司" {
		t.Fatalf("company name mismatch: got=%q", got)
	}
}
