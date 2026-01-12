package services

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestExtractInvoiceArtifactsFromEmail_MultipartAlternativeBase64BodyContainsInvoiceLink(t *testing.T) {
	plain := "点击链接查看发票：https://nnfp.jss.com.cn/8_CszRwjaw-FBnv"
	html := `<div>
  <a href="https://nnfp.jss.com.cn/8_CszRwjaw-FBnv">下载发票</a>
  <a href="https://fp.nuonuo.com/#/">诺诺发票</a>
  <img src="http://linktrace.triggerdelivery.com/u/o1/N132-XXX" height="1" width="1">
</div>`

	raw := strings.ReplaceAll(`From: invoice@info.nuonuo.com
To: user@example.com
Subject: test
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="b"

--b
Content-Type: text/plain; charset="utf-8"
Content-Transfer-Encoding: base64

`+base64.StdEncoding.EncodeToString([]byte(plain))+`
--b
Content-Type: text/html; charset="utf-8"
Content-Transfer-Encoding: base64

`+base64.StdEncoding.EncodeToString([]byte(html))+`
--b--
`, "\n", "\r\n")

	mr, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, _, bodyText, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bodyText, "https://nnfp.jss.com.cn/8_CszRwjaw-FBnv") {
		t.Fatalf("expected invoice link in extracted body text, got: %q", bodyText)
	}

	got := bestInvoicePreviewURLFromBody(bodyText)
	if got != "https://nnfp.jss.com.cn/8_CszRwjaw-FBnv" {
		t.Fatalf("unexpected preview url: %q", got)
	}
}

