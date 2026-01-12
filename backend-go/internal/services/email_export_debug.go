package services

import (
	"bytes"
	"context"
	"regexp"
	"strings"

	"github.com/emersion/go-message/mail"
)

type EmailLogDebugExport struct {
	Filename                string   `json:"filename"`
	BodyTextLen             int      `json:"bodyTextLen"`
	BodyTextSample          string   `json:"bodyTextSample"`
	BestInvoicePreviewURL   string   `json:"bestInvoicePreviewUrl"`
	BestPreviewURLFromText  string   `json:"bestPreviewUrlFromText"`
	FirstURLFromText        string   `json:"firstUrlFromText"`
	FoundXMLURLFromBodyText *string  `json:"foundXmlUrlFromBodyText,omitempty"`
	FoundPDFURLFromBodyText *string  `json:"foundPdfUrlFromBodyText,omitempty"`
	AllURLsFromBodyText     []string `json:"allUrlsFromBodyText"`
}

func (s *EmailService) ExportEmailLogDebugCtx(ctx context.Context, ownerUserID string, logID string) (*EmailLogDebugExport, error) {
	filename, emlBytes, err := s.ExportEmailLogEMLCtx(ctx, ownerUserID, logID)
	if err != nil {
		return nil, err
	}

	mr, err := mail.CreateReader(bytes.NewReader(emlBytes))
	if err != nil {
		return nil, err
	}

	_, _, _, _, bodyText, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		return nil, err
	}

	foundXML, foundPDF := extractInvoiceLinksFromText(bodyText)
	out := &EmailLogDebugExport{
		Filename:                filename,
		BodyTextLen:             len(bodyText),
		BodyTextSample:          sampleString(bodyText, 24*1024),
		BestInvoicePreviewURL:   bestInvoicePreviewURLFromBody(bodyText),
		BestPreviewURLFromText:  bestPreviewURLFromText(bodyText),
		FirstURLFromText:        firstURLFromText(bodyText),
		FoundXMLURLFromBodyText: foundXML,
		FoundPDFURLFromBodyText: foundPDF,
		AllURLsFromBodyText:     allURLsFromText(bodyText, 80),
	}
	return out, nil
}

func sampleString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func allURLsFromText(body string, limit int) []string {
	body = strings.TrimSpace(body)
	if body == "" || limit == 0 {
		return nil
	}
	if limit < 0 {
		limit = 0
	}

	urlRe := regexp.MustCompile(`(?i)(https?://[^\s<>"'()]+|//[^\s<>"'()]+)`)
	raw := urlRe.FindAllString(body, -1)
	if len(raw) == 0 {
		return nil
	}

	clean := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimRight(s, ">)].,;\"'")
		if strings.HasPrefix(s, "//") {
			s = "https:" + s
		}
		return s
	}

	seen := map[string]struct{}{}
	add := func(u string, out *[]string) {
		u = clean(u)
		if u == "" {
			return
		}
		key := strings.ToLower(u)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		*out = append(*out, u)
	}

	out := make([]string, 0, minInt(len(raw), limit))
	for _, u := range raw {
		if limit > 0 && len(out) >= limit {
			break
		}
		add(u, &out)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
