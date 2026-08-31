package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"image"
	_ "image/png"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/emailmime"
)

func TestSyntheticEmailContainsOneExactPNGAttachment(t *testing.T) {
	imageBytes := syntheticAttachmentPNG()
	configuration, format, err := image.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil || format != "png" || configuration.Width != 1 || configuration.Height != 1 {
		t.Fatalf("embedded PNG = %dx%d/%q, %v", configuration.Width, configuration.Height, format, err)
	}
	raw := buildSyntheticMIME(imageBytes)
	parsed := (emailmime.Parser{}).Parse(raw)
	if parsed.BlockedCode != "" || parsed.Subject != "M4 recovery fixture" || parsed.SenderAddress != "sender@example.invalid" || len(parsed.Attachments) != 1 {
		t.Fatalf("parsed synthetic message = %#v", parsed)
	}
	attachment := parsed.Attachments[0]
	if attachment.Name != "recovery.png" || attachment.MIME != "image/png" || attachment.Disposition != "attachment" || !bytes.Equal(attachment.Content, imageBytes) {
		t.Fatalf("parsed synthetic attachment = %#v", attachment)
	}
	rawHash := sha256.Sum256(raw)
	imageHash := sha256.Sum256(imageBytes)
	if bytes.Equal(rawHash[:], imageHash[:]) {
		t.Fatal("raw message and attachment unexpectedly share a digest")
	}
}

func TestSafeRecoveryOutputOmitsIdentifiersAndHashes(t *testing.T) {
	result := emailFixtureResult{
		Kind: "m4-recovery-email-fixture", Version: 1,
		MessageID: "message-private", AttachmentID: "attachment-private",
		DocumentID: "document-private", JobID: "job-private",
		RawSHA256: strings.Repeat("a", 64), AttachmentSHA256: strings.Repeat("b", 64), Passed: true,
	}
	encoded, err := json.Marshal(safeRecoveryOutput(result))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private", strings.Repeat("a", 64), strings.Repeat("b", 64), "sha256"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("safe recovery output contains %q: %s", forbidden, encoded)
		}
	}
}

func TestSafeRecoveryErrorCodeOmitsProtectedDetails(t *testing.T) {
	detail := "document document-private at /secure/private differs from " + strings.Repeat("a", 64)
	code := safeRecoveryErrorCode(errors.New(detail))
	if code != "document_shape_invalid" || strings.Contains(code, "private") || strings.Contains(code, "secure") || strings.Contains(code, strings.Repeat("a", 64)) {
		t.Fatalf("safe recovery error code = %q", code)
	}
}
