package services

import (
	"testing"

	"github.com/emersion/go-imap"
)

func TestCountPDFAttachments_UsesFilenameCaseInsensitiveParams(t *testing.T) {
	bs := &imap.BodyStructure{
		MIMEType:    "application",
		MIMESubType: "octet-stream",
		Params: map[string]string{
			"NAME": "test.PDF",
		},
		Extended: true,
	}

	has, cnt := countPDFAttachments(bs)
	if has != 1 || cnt != 1 {
		t.Fatalf("expected 1 pdf attachment, got has=%d cnt=%d", has, cnt)
	}
}

func TestCountPDFAttachments_WalksMultipart(t *testing.T) {
	bs := &imap.BodyStructure{
		MIMEType:    "multipart",
		MIMESubType: "mixed",
		Parts: []*imap.BodyStructure{
			{MIMEType: "text", MIMESubType: "plain"},
			{
				MIMEType:    "application",
				MIMESubType: "pdf",
			},
		},
	}

	has, cnt := countPDFAttachments(bs)
	if has != 1 || cnt != 1 {
		t.Fatalf("expected 1 pdf attachment, got has=%d cnt=%d", has, cnt)
	}
}

