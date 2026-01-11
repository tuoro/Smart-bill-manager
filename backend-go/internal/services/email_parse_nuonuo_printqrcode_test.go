package services

import "testing"

func TestNuonuoBuildIvcDetailRequestFromPrintURL_DefaultEndpoint(t *testing.T) {
	u := "https://nnfp.jss.com.cn/scan-invoice/printQrcode?paramList=AAA!!!BBB!false&aliView=true&shortLinkSource=1&wxApplet=0"
	endpoint, form, err := nuonuoBuildIvcDetailRequestFromPrintURL(u)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if endpoint != "/scan2/getIvcDetailShow.do" {
		t.Fatalf("unexpected endpoint: %q", endpoint)
	}
	if form.Get("paramList") != "AAA!!!BBB!false" {
		t.Fatalf("unexpected paramList: %q", form.Get("paramList"))
	}
	if form.Get("aliView") != "true" {
		t.Fatalf("unexpected aliView: %q", form.Get("aliView"))
	}
	if form.Get("shortLinkSource") != "1" {
		t.Fatalf("unexpected shortLinkSource: %q", form.Get("shortLinkSource"))
	}
	if form.Get("invoiceDetailMiddleUri") != u {
		t.Fatalf("unexpected invoiceDetailMiddleUri: %q", form.Get("invoiceDetailMiddleUri"))
	}
}

func TestNuonuoBuildIvcDetailRequestFromPrintURL_OuterPageReq(t *testing.T) {
	u := "https://nnfp.jss.com.cn/scan-invoice/printQrcode?isOuterPageReq=true&paramList=AAA!!!BBB!false"
	endpoint, form, err := nuonuoBuildIvcDetailRequestFromPrintURL(u)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if endpoint != "/invoice/scan/IvcDetail.do" {
		t.Fatalf("unexpected endpoint: %q", endpoint)
	}
	if form.Get("paramList") != "AAA!!!BBB!false" {
		t.Fatalf("unexpected paramList: %q", form.Get("paramList"))
	}
}

func TestNuonuoBuildIvcDetailRequestFromPrintURL_MissingParamList(t *testing.T) {
	_, _, err := nuonuoBuildIvcDetailRequestFromPrintURL("https://nnfp.jss.com.cn/scan-invoice/printQrcode")
	if err == nil {
		t.Fatalf("expected error")
	}
}

