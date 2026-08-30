package emailmime

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestParserExtractsOnlySafeHeadersAndAttachments(t *testing.T) {
	t.Parallel()
	png := []byte("synthetic-png-bytes")
	raw := strings.Join([]string{
		"From: Synthetic Sender <sender@example.invalid>",
		"To: archive@example.invalid",
		"Date: Sun, 31 Aug 2026 10:20:30 +0800",
		"Subject: =?UTF-8?B?5ZCI5oiQ6LSm5Y2V?=",
		"Content-Type: multipart/mixed; boundary=outer",
		"",
		"--outer",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<img src=https://remote.invalid/private><script>ignored()</script>",
		"--outer",
		"Content-Type: multipart/related; boundary=inner",
		"",
		"--inner",
		"Content-Type: image/png; name=invoice.png",
		"Content-Disposition: inline; filename=invoice.png",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString(png),
		"--inner--",
		"--outer",
		"Content-Type: text/plain; name=notes.txt",
		"Content-Disposition: attachment; filename=notes.txt",
		"",
		"synthetic note",
		"--outer--",
		"",
	}, "\r\n")
	result := (Parser{}).Parse([]byte(raw))
	if result.BlockedCode != "" || result.Subject != "合成账单" || result.SenderAddress != "sender@example.invalid" {
		t.Fatalf("message = %#v", result)
	}
	if result.SentAt == nil || result.SentAt.Format("2006-01-02T15:04:05Z") != "2026-08-31T02:20:30Z" {
		t.Fatalf("sent_at = %v", result.SentAt)
	}
	if len(result.Attachments) != 2 || result.Attachments[0].Disposition != "inline" ||
		result.Attachments[0].Name != "invoice.png" || string(result.Attachments[0].Content) != string(png) ||
		result.Attachments[1].Name != "notes.txt" {
		t.Fatalf("attachments = %#v", result.Attachments)
	}
}

func TestParserBlocksInvalidAndBoundedMIMEWithoutPartialAttachments(t *testing.T) {
	t.Parallel()
	invalid := (Parser{}).Parse([]byte("not a mail header"))
	if invalid.BlockedCode != "email_mime_invalid" || len(invalid.Attachments) != 0 {
		t.Fatalf("invalid = %#v", invalid)
	}

	var parts strings.Builder
	parts.WriteString("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n")
	for index := 0; index <= domain.MaxEmailAttachments; index++ {
		fmt.Fprintf(&parts, "--x\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=f-%03d.txt\r\n\r\nvalue\r\n", index)
	}
	parts.WriteString("--x--\r\n")
	limited := (Parser{}).Parse([]byte(parts.String()))
	if limited.BlockedCode != "email_attachment_limit_exceeded" || len(limited.Attachments) != 0 {
		t.Fatalf("limited = %#v", limited)
	}

	deep := "From: sender@example.invalid\r\nContent-Type: text/plain\r\n\r\nbody"
	for depth := 0; depth < domain.MaxEmailMIMEDepth; depth++ {
		boundary := fmt.Sprintf("b%d", depth)
		deep = fmt.Sprintf("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\n%s\r\n--%s--\r\n", boundary, boundary, deep, boundary)
	}
	deepResult := (Parser{}).Parse([]byte(deep))
	if deepResult.BlockedCode != "email_mime_depth_exceeded" || len(deepResult.Attachments) != 0 {
		t.Fatalf("deep = %#v", deepResult)
	}
}

func TestParserBlocksUnknownTransferEncoding(t *testing.T) {
	t.Parallel()
	raw := "From: sender@example.invalid\r\nContent-Type: application/pdf; name=invoice.pdf\r\nContent-Disposition: attachment; filename=invoice.pdf\r\nContent-Transfer-Encoding: x-private\r\n\r\nbytes"
	result := (Parser{}).Parse([]byte(raw))
	if result.BlockedCode != "email_attachment_decode_failed" || len(result.Attachments) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParserAcceptsFiftyAttachmentsAndBlocksPartTwoHundredOne(t *testing.T) {
	t.Parallel()

	var attachments strings.Builder
	attachments.WriteString("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=a\r\n\r\n")
	for index := 0; index < domain.MaxEmailAttachments; index++ {
		fmt.Fprintf(&attachments, "--a\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=f-%03d.txt\r\n\r\nvalue\r\n", index)
	}
	attachments.WriteString("--a--\r\n")
	accepted := (Parser{}).Parse([]byte(attachments.String()))
	if accepted.BlockedCode != "" || len(accepted.Attachments) != domain.MaxEmailAttachments {
		t.Fatalf("fifty attachments = %#v", accepted)
	}

	var parts strings.Builder
	parts.WriteString("From: sender@example.invalid\r\nContent-Type: multipart/mixed; boundary=p\r\n\r\n")
	for index := 0; index < domain.MaxEmailMIMEParts; index++ {
		fmt.Fprintf(&parts, "--p\r\nContent-Type: text/plain\r\n\r\npart-%03d\r\n", index)
	}
	parts.WriteString("--p--\r\n")
	blocked := (Parser{}).Parse([]byte(parts.String()))
	if blocked.BlockedCode != "email_mime_part_limit_exceeded" || len(blocked.Attachments) != 0 {
		t.Fatalf("part 201 = %#v", blocked)
	}
}
