package services

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestNormalizeInvoiceXMLBytes_ZipContainsXML(t *testing.T) {
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
  </EInvoiceData>
  <TaxSupervisionInfo>
    <InvoiceNumber>99000000000000000201</InvoiceNumber>
    <IssueTime>2026-08-05 13:02:50</IssueTime>
  </TaxSupervisionInfo>
</EInvoice>
`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("xml/invoice.xml")
	if err != nil {
		t.Fatalf("zip create entry: %v", err)
	}
	if _, err := w.Write([]byte(xmlStr)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	normalized, entry, err := normalizeInvoiceXMLBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("normalizeInvoiceXMLBytes: %v", err)
	}
	if entry != "xml/invoice.xml" {
		t.Fatalf("expected entry %q got %q", "xml/invoice.xml", entry)
	}

	extracted, err := parseInvoiceXMLToExtracted(normalized)
	if err != nil {
		t.Fatalf("parseInvoiceXMLToExtracted: %v", err)
	}
	if extracted.InvoiceNumber == nil || *extracted.InvoiceNumber != "99000000000000000201" {
		t.Fatalf("invoice number mismatch: %#v", extracted.InvoiceNumber)
	}
}
