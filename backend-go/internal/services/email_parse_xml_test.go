package services

import "testing"

func TestParseInvoiceXMLToExtracted_EInvoice(t *testing.T) {
	xmlStr := `
<!-- SYNTHETIC / 纯合成测试数据 -->
<EInvoice>
  <Header>
    <EIid>99000000000000000201</EIid>
    <EInvoiceTag>SYNTHETIC-TAG-01</EInvoiceTag>
    <Version>0.2</Version>
  </Header>
  <EInvoiceData>
    <SellerInformation>
      <SellerIdNum>91110000SYNTH00001</SellerIdNum>
      <SellerName>纯合成电器销售有限公司</SellerName>
    </SellerInformation>
    <BuyerInformation>
      <BuyerName>纯合成购买方丙</BuyerName>
    </BuyerInformation>
    <BasicInformation>
      <TotalAmWithoutTax>100.00</TotalAmWithoutTax>
      <TotalTaxAm>6.00</TotalTaxAm>
      <TotalTax-includedAmount>106.00</TotalTax-includedAmount>
      <RequestTime>2026-08-05 13:02:50</RequestTime>
    </BasicInformation>
    <IssuItemInformation>
      <ItemName>*纯合成设备*测试空调 合成型号 SYN-35GW/TEST0001</ItemName>
      <SpecMod>SYN-35GW/TEST0001</SpecMod>
      <MeaUnits>套</MeaUnits>
      <Quantity>2.00000000</Quantity>
    </IssuItemInformation>
  </EInvoiceData>
  <TaxSupervisionInfo>
    <InvoiceNumber>99000000000000000201</InvoiceNumber>
    <IssueTime>2026-08-05 13:02:50</IssueTime>
  </TaxSupervisionInfo>
</EInvoice>
`

	extracted, err := parseInvoiceXMLToExtracted([]byte(xmlStr))
	if err != nil {
		t.Fatalf("parseInvoiceXMLToExtracted err: %v", err)
	}
	if extracted.InvoiceNumber == nil || *extracted.InvoiceNumber != "99000000000000000201" {
		t.Fatalf("invoice number mismatch: %#v", extracted.InvoiceNumber)
	}
	if extracted.InvoiceDate == nil || *extracted.InvoiceDate != "2026-08-05" {
		t.Fatalf("invoice date mismatch: %#v", extracted.InvoiceDate)
	}
	if extracted.Amount == nil || (*extracted.Amount < 105.999 || *extracted.Amount > 106.001) {
		t.Fatalf("amount mismatch: %#v", extracted.Amount)
	}
	if extracted.TaxAmount == nil || (*extracted.TaxAmount < 5.999 || *extracted.TaxAmount > 6.001) {
		t.Fatalf("tax amount mismatch: %#v", extracted.TaxAmount)
	}
	if extracted.SellerName == nil || *extracted.SellerName != "纯合成电器销售有限公司" {
		t.Fatalf("seller mismatch: %#v", extracted.SellerName)
	}
	if extracted.BuyerName == nil || *extracted.BuyerName != "纯合成购买方丙" {
		t.Fatalf("buyer mismatch: %#v", extracted.BuyerName)
	}
	if len(extracted.Items) != 1 {
		t.Fatalf("items count mismatch: %d", len(extracted.Items))
	}
	item := extracted.Items[0]
	if item.Name == "" || item.Spec != "SYN-35GW/TEST0001" || item.Unit != "套" || item.Quantity == nil || (*item.Quantity < 1.999 || *item.Quantity > 2.001) {
		t.Fatalf("item mismatch: %+v", item)
	}
}
