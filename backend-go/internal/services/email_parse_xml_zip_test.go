package services

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestNormalizeInvoiceXMLBytes_ZipContainsXML(t *testing.T) {
	xmlStr := `
<EInvoice>
  <Header>
    <EIid>25327000001739485410</EIid>
    <EInvoiceTag>SWEI3200</EInvoiceTag>
    <Version>0.2</Version>
  </Header>
  <EInvoiceData>
    <SellerInformation>
      <SellerIdNum>913205830880018839</SellerIdNum>
      <SellerName>昆山京东尚信贸易有限公司</SellerName>
    </SellerInformation>
    <BuyerInformation>
      <BuyerName>乌洪军</BuyerName>
    </BuyerInformation>
    <BasicInformation>
      <TotalAmWithoutTax>5838.94</TotalAmWithoutTax>
      <TotalTaxAm>759.06</TotalTaxAm>
      <TotalTax-includedAmount>6598.00</TotalTax-includedAmount>
      <RequestTime>2025-12-29 13:02:50</RequestTime>
    </BasicInformation>
  </EInvoiceData>
  <TaxSupervisionInfo>
    <InvoiceNumber>25327000001739485410</InvoiceNumber>
    <IssueTime>2025-12-29 13:02:50</IssueTime>
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
	if extracted.InvoiceNumber == nil || *extracted.InvoiceNumber != "25327000001739485410" {
		t.Fatalf("invoice number mismatch: %#v", extracted.InvoiceNumber)
	}
}

