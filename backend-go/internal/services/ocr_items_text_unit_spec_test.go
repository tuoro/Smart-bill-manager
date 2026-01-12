package services

import "testing"

func TestExtractInvoiceLineItems_Text_PeelsTrailingUnitSpecTokens(t *testing.T) {
	text := `
【购买方】
项目名称 规格型号 单位 数量
【明细】
*餐饮服务*餐饮服务项项1
价税合计（小写） ￥ 2632.00
`

	items := extractInvoiceLineItems(text)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
	}
	it := items[0]
	if it.Quantity == nil || *it.Quantity != 1 {
		t.Fatalf("unexpected quantity: %+v", it)
	}
	if it.Unit != "项" || it.Spec != "项" {
		t.Fatalf("expected unit/spec '项', got: %+v", it)
	}
	if it.Name == "" || it.Name == "餐饮服务餐饮服务项项" || it.Name == "*餐饮服务*餐饮服务项项" {
		t.Fatalf("expected cleaned name without tail tokens, got: %q", it.Name)
	}
}

