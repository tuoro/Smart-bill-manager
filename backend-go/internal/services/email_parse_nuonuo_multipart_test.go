package services

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestExtractInvoiceArtifactsFromEmail_MultipartAlternativeBase64BodyContainsInvoiceLink(t *testing.T) {
	plain := "SYNTHETIC / 纯合成测试数据\n点击链接查看发票：https://nnfp.jss.com.cn/synthetic-preview-token-0001"
	html := `<div>
  <a href="https://nnfp.jss.com.cn/synthetic-preview-token-0001">下载发票</a>
  <a href="https://fp.nuonuo.com/#/">诺诺发票</a>
  <img src="http://linktrace.triggerdelivery.com/u/o1/SYNTHETIC-PIXEL" height="1" width="1">
</div>`

	raw := strings.ReplaceAll(`From: invoice@info.nuonuo.com
To: user@example.com
Subject: synthetic test
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
	if !strings.Contains(bodyText, "https://nnfp.jss.com.cn/synthetic-preview-token-0001") {
		t.Fatalf("expected invoice link in extracted body text, got: %q", bodyText)
	}

	got := bestInvoicePreviewURLFromBody(bodyText)
	if got != "https://nnfp.jss.com.cn/synthetic-preview-token-0001" {
		t.Fatalf("unexpected preview url: %q", got)
	}
}
