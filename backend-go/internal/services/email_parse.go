package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

func init() {
	// Enable decoding of common Chinese email charsets (e.g. gb18030) in go-message mail parsing.
	message.CharsetReader = charset.NewReaderLabel
}

const (
	emailParseMaxPDFBytes  = 20 * 1024 * 1024
	emailParseMaxXMLBytes  = 5 * 1024 * 1024
	emailParseMaxTextBytes = 512 * 1024
	emailParseMaxPageBytes = 2 * 1024 * 1024
)

type emailHeaderLike interface {
	ContentDisposition() (disp string, params map[string]string, err error)
	ContentType() (t string, params map[string]string, err error)
}

func bestEmailPartFilename(header interface{}, fallback string) string {
	if header == nil {
		return strings.TrimSpace(fallback)
	}
	// Try go-message helpers first; they decode RFC 2047 encoded words.
	if fh, ok := header.(interface{ Filename() (string, error) }); ok {
		if v, err := fh.Filename(); err == nil {
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		}
	}
	if hl, ok := header.(emailHeaderLike); ok {
		if s := strings.TrimSpace(extractFilenameFromEmailHeader(hl)); s != "" {
			return s
		}
	}
	return strings.TrimSpace(fallback)
}

func extractFilenameFromEmailHeader(h emailHeaderLike) string {
	if h == nil {
		return ""
	}
	// Try Content-Disposition filename=...
	if disp, params, err := h.ContentDisposition(); err == nil && strings.TrimSpace(disp) != "" {
		if v := strings.TrimSpace(params["filename"]); v != "" {
			return v
		}
	}
	// Try Content-Type name=...
	if _, params, err := h.ContentType(); err == nil {
		if v := strings.TrimSpace(params["name"]); v != "" {
			return v
		}
	}
	return ""
}

func isPDFEmailHeader(h emailHeaderLike) (isPDF bool, filename string) {
	if h == nil {
		return false, ""
	}
	ct, _, _ := h.ContentType()
	ct = strings.ToLower(strings.TrimSpace(ct))
	filename = strings.TrimSpace(extractFilenameFromEmailHeader(h))
	filenameLower := strings.ToLower(filename)
	// Prefer MIME type; some providers omit a proper filename or extension.
	if ct == "application/pdf" {
		return true, filename
	}
	if strings.HasSuffix(filenameLower, ".pdf") || strings.Contains(filenameLower, ".pdf?") {
		return true, filename
	}
	return false, filename
}

func isXMLEmailHeader(h emailHeaderLike) (isXML bool, filename string) {
	if h == nil {
		return false, ""
	}
	ct, _, _ := h.ContentType()
	ct = strings.ToLower(strings.TrimSpace(ct))
	filename = strings.TrimSpace(extractFilenameFromEmailHeader(h))
	filenameLower := strings.ToLower(filename)
	if strings.Contains(ct, "xml") {
		return true, filename
	}
	if strings.HasSuffix(filenameLower, ".xml") || strings.Contains(filenameLower, ".xml?") {
		return true, filename
	}
	return false, filename
}

type emailBinaryAttachment struct {
	Filename string
	Bytes    []byte
}

func isItineraryPDFName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	// Common wording in Chinese invoice emails.
	if strings.Contains(n, "行程单") || strings.Contains(n, "电子行程单") {
		return true
	}
	// Some providers use English.
	if strings.Contains(n, "itinerary") {
		return true
	}
	return false
}

func contentTypeLowerFromHeader(h interface{}) string {
	if h == nil {
		return ""
	}
	if hl, ok := h.(emailHeaderLike); ok {
		ct, _, _ := hl.ContentType()
		return strings.ToLower(strings.TrimSpace(ct))
	}
	// Fallback for header types that don't implement ContentType() but still provide raw access.
	if gh, ok := h.(interface{ Get(string) string }); ok {
		raw := strings.TrimSpace(gh.Get("Content-Type"))
		if raw == "" {
			return ""
		}
		if i := strings.Index(raw, ";"); i >= 0 {
			raw = raw[:i]
		}
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return ""
}

func extractInvoiceArtifactsFromEmail(mr *mail.Reader) (pdfFilename string, pdfBytes []byte, xmlBytes []byte, itineraryPDFs []emailBinaryAttachment, bodyText string, err error) {
	if mr == nil {
		return "", nil, nil, nil, "", fmt.Errorf("nil mail reader")
	}

	textParts := make([]string, 0, 8)
	pdfParts := make([]emailBinaryAttachment, 0, 4)

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, nil, nil, "", err
		}

		var (
			ct string
		)
		ct = contentTypeLowerFromHeader(part.Header)
		if hl, ok := part.Header.(emailHeaderLike); ok {

			// Some providers embed the actual invoice email as a forwarded message/rfc822 part.
			if ct == "message/rfc822" {
				if inner, err := mail.CreateReader(part.Body); err == nil {
					name2, pdf2, xml2, itins2, text2, err2 := extractInvoiceArtifactsFromEmail(inner)
					if err2 != nil {
						return "", nil, nil, nil, "", err2
					}
					if pdf2 != nil {
						pdfParts = append(pdfParts, emailBinaryAttachment{Filename: name2, Bytes: pdf2})
					}
					if xmlBytes == nil && xml2 != nil {
						xmlBytes = xml2
					}
					if len(itins2) > 0 {
						itineraryPDFs = append(itineraryPDFs, itins2...)
					}
					if strings.TrimSpace(text2) != "" && len(textParts) < 12 {
						textParts = append(textParts, text2)
					}
				}
				continue
			}

			// Detect PDFs/XMLs regardless of disposition; some servers omit Content-Disposition.
			if ok, hinted := isPDFEmailHeader(hl); ok {
				filename := bestEmailPartFilename(part.Header, hinted)
				b, err := readWithLimit(part.Body, emailParseMaxPDFBytes)
				if err != nil {
					return "", nil, nil, nil, "", err
				}
				pdfParts = append(pdfParts, emailBinaryAttachment{Filename: filename, Bytes: b})
				continue
			}
			if xmlBytes == nil {
				if ok, hinted := isXMLEmailHeader(hl); ok {
					filename := bestEmailPartFilename(part.Header, hinted)
					_ = filename // keep for future; currently XML filename isn't stored
					b, err := readWithLimit(part.Body, emailParseMaxXMLBytes)
					if err != nil {
						return "", nil, nil, nil, "", err
					}
					xmlBytes = b
					continue
				}
			}
		}

		// Collect body text for link parsing (xml/pdf download URLs).
		if len(textParts) < 12 && strings.HasPrefix(ct, "text/") {
			if b, err := readWithLimit(part.Body, emailParseMaxTextBytes); err == nil {
				s := strings.TrimSpace(string(b))
				if s != "" {
					textParts = append(textParts, s)
				}
			}
		}
	}

	// Choose the best PDF as the actual invoice PDF; keep itinerary PDFs as extra attachments.
	if len(pdfParts) > 0 {
		bestIdx := 0
		bestScore := -9999
		for i, p := range pdfParts {
			n := strings.ToLower(strings.TrimSpace(p.Filename))
			score := 0
			if strings.Contains(n, "电子发票") {
				score += 40
			}
			if strings.Contains(n, "发票") {
				score += 25
			}
			if isItineraryPDFName(n) {
				score -= 80
			}
			if strings.HasSuffix(n, ".pdf") {
				score += 1
			}
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}

		pdfFilename = pdfParts[bestIdx].Filename
		pdfBytes = pdfParts[bestIdx].Bytes
		for i, p := range pdfParts {
			if i == bestIdx {
				continue
			}
			if isItineraryPDFName(p.Filename) {
				itineraryPDFs = append(itineraryPDFs, p)
			}
		}
	}

	return pdfFilename, pdfBytes, xmlBytes, itineraryPDFs, strings.Join(textParts, "\n"), nil
}

func (s *EmailService) ParseEmailLog(ownerUserID string, logID string) (*models.Invoice, error) {
	return s.ParseEmailLogCtx(context.Background(), ownerUserID, logID)
}

func shouldShortCircuitEmailLogParse(status string, parsedInvoiceID *string) bool {
	if !strings.EqualFold(strings.TrimSpace(status), "parsed") {
		return false
	}
	if parsedInvoiceID == nil {
		return false
	}
	return strings.TrimSpace(*parsedInvoiceID) != ""
}

func (s *EmailService) ParseEmailLogCtx(ctx context.Context, ownerUserID string, logID string) (*models.Invoice, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("missing owner_user_id")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	logID = strings.TrimSpace(logID)
	if logID == "" {
		return nil, fmt.Errorf("missing log id")
	}

	logRow, err := s.repo.FindLogByIDCtx(ctx, logID)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(logRow.OwnerUserID) != ownerUserID {
		// Avoid leaking other users' log ids.
		return nil, gorm.ErrRecordNotFound
	}

	// Only short-circuit when the log is already successfully parsed.
	// Some older records may have inconsistent status/id, and users should still be able to retry.
	if shouldShortCircuitEmailLogParse(logRow.Status, logRow.ParsedInvoiceID) {
		inv, err := s.invoiceService.GetByID(strings.TrimSpace(logRow.OwnerUserID), strings.TrimSpace(*logRow.ParsedInvoiceID))
		if err == nil {
			return inv, nil
		}
		// If user deleted the invoice, clear the pointer so they can parse again.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = s.repo.UpdateLog(logID, map[string]interface{}{
				"parsed_invoice_id": nil,
				"status":           "received",
				"parse_error":      nil,
			})
		} else {
			return nil, err
		}
	}

	if logRow.MessageUID <= 0 {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": "missing message uid (old log record); please re-fetch emails",
		})
		return nil, fmt.Errorf("missing message uid (old log record); please re-fetch emails")
	}

	_ = s.repo.UpdateLog(logID, map[string]interface{}{
		"status":      "parsing",
		"parse_error": nil,
	})

	cfg, err := s.repo.FindConfigByIDCtx(ctx, logRow.EmailConfigID)
	if err != nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": "email config not found",
		})
		return nil, err
	}
	if strings.TrimSpace(cfg.OwnerUserID) != ownerUserID {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": "email config owner mismatch",
		})
		return nil, fmt.Errorf("email config owner mismatch")
	}

	addr := fmt.Sprintf("%s:%d", cfg.IMAPHost, cfg.IMAPPort)
	// #nosec G402 - InsecureSkipVerify is intentional to support self-signed certs
	c, err := client.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": fmt.Sprintf("imap connect failed: %v", err),
		})
		return nil, err
	}
	defer c.Logout()

	if err := c.Login(cfg.Email, cfg.Password); err != nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": fmt.Sprintf("imap login failed: %v", err),
		})
		return nil, err
	}

	mailbox := strings.TrimSpace(logRow.Mailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if _, err := c.Select(mailbox, false); err != nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": fmt.Sprintf("select mailbox failed: %v", err),
		})
		return nil, err
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(logRow.MessageUID)

	section := &imap.BodySectionName{Peek: true} // BODY.PEEK[]
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, section.FetchItem()}
	msgCh := make(chan *imap.Message, 1)

	go func() {
		_ = c.UidFetch(seqSet, items, msgCh)
	}()

	msg := <-msgCh
	if msg == nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": "message not found (uid fetch returned nil)",
		})
		return nil, fmt.Errorf("message not found")
	}

	r := msg.GetBody(section)
	if r == nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": "message body not available",
		})
		return nil, fmt.Errorf("message body not available")
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": fmt.Sprintf("parse email failed: %v", err),
		})
		return nil, err
	}

	var (
		pdfFilename string
		pdfBytes    []byte
		xmlBytes    []byte
		itineraryPDFs []emailBinaryAttachment
	)

	pdfFilename, pdfBytes, xmlBytes, itineraryPDFs, bodyText, err := extractInvoiceArtifactsFromEmail(mr)
	if err != nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": fmt.Sprintf("read email part failed: %v", err),
		})
		return nil, err
	}

	xmlURL := logRow.InvoiceXMLURL
	pdfURL := logRow.InvoicePDFURL

	foundXML, foundPDF := extractInvoiceLinksFromText(bodyText)
	if xmlURL == nil && foundXML != nil {
		xmlURL = foundXML
	}
	if pdfURL == nil && foundPDF != nil {
		pdfURL = foundPDF
	}

	// Some providers send only a preview-page link that requires clicking "下载PDF/XML".
	// Best-effort: fetch the page and try to discover direct download URLs.
	previewResolveErr := ""
	{
		// If older builds persisted "invoice_pdf_url/xml_url" with a non-direct/irrelevant URL, do not let it block
		// re-parsing. We'll treat such placeholders as candidates for preview resolving (or ignore if obviously bad).
		if pdfURL != nil && isBadEmailPreviewURL(*pdfURL) {
			pdfURL = nil
		}
		if xmlURL != nil && isBadEmailPreviewURL(*xmlURL) {
			xmlURL = nil
		}

		needPDF := pdfBytes == nil && (pdfURL == nil || !isDirectInvoicePDFURL(*pdfURL))
		needXML := xmlBytes == nil && (xmlURL == nil || !isDirectInvoiceXMLURL(*xmlURL))
		if needPDF || needXML {
			addCandidate := func(candidates *[]string, u string) {
				u = strings.TrimSpace(u)
				if u == "" || isBadEmailPreviewURL(u) {
					return
				}
				for _, existing := range *candidates {
					if strings.EqualFold(existing, u) {
						return
					}
				}
				*candidates = append(*candidates, u)
			}

		var previewCandidates []string
			// Always prefer picking from body text (it tends to be the "real" invoice link shown to users).
			addCandidate(&previewCandidates, bestInvoicePreviewURLFromBody(bodyText))
			addCandidate(&previewCandidates, bestPreviewURLFromText(bodyText))

			// Include any persisted placeholder URLs as fallbacks.
			if pdfURL != nil && !isDirectInvoicePDFURL(*pdfURL) {
				addCandidate(&previewCandidates, *pdfURL)
			}
			if xmlURL != nil && !isDirectInvoiceXMLURL(*xmlURL) {
				addCandidate(&previewCandidates, *xmlURL)
			}

			// Last resort: take the first URL in the email text.
			if len(previewCandidates) == 0 {
				addCandidate(&previewCandidates, firstURLFromText(bodyText))
			}

			var resolveErrs []string
			for _, previewURL := range previewCandidates {
				// Direct links: accept without extra fetching.
				if needPDF && isDirectInvoicePDFURL(previewURL) {
					pdfURL = ptrString(previewURL)
					needPDF = false
				}
				if needXML && isDirectInvoiceXMLURL(previewURL) {
					xmlURL = ptrString(previewURL)
					needXML = false
				}
				if !needPDF && !needXML {
					break
				}

				resXML, resPDF, err := resolveInvoiceDownloadLinksFromPreviewURLCtx(ctx, previewURL)
				if err != nil {
					resolveErrs = append(resolveErrs, fmt.Sprintf("%v (preview_url=%s)", err, previewURL))
					continue
				}

				// Override preview-like placeholders with resolved direct download URLs.
				if needXML && resXML != nil && strings.TrimSpace(*resXML) != "" {
					xmlURL = resXML
					needXML = false
				}
				if needPDF && resPDF != nil && strings.TrimSpace(*resPDF) != "" {
					pdfURL = resPDF
					needPDF = false
				}
				if !needPDF && !needXML {
					break
				}
			}

			if len(resolveErrs) > 0 && (needPDF || needXML) {
				previewResolveErr = resolveErrs[len(resolveErrs)-1]
			}

			// Persist resolved direct links (or clear bad placeholders).
			if logID != "" {
				_ = s.repo.UpdateLog(logID, map[string]interface{}{
					"invoice_xml_url": func() interface{} {
						if xmlURL == nil {
							return nil
						}
						if strings.TrimSpace(*xmlURL) == "" {
							return nil
						}
						return *xmlURL
					}(),
					"invoice_pdf_url": func() interface{} {
						if pdfURL == nil {
							return nil
						}
						if strings.TrimSpace(*pdfURL) == "" {
							return nil
						}
						return *pdfURL
					}(),
				})
			}
		}
	}

	// Ensure we have the PDF bytes for preview (either from attachment or from a PDF download link).
	if pdfBytes == nil {
		if pdfURL == nil || !isDirectInvoicePDFURL(*pdfURL) {
			if strings.TrimSpace(previewResolveErr) != "" {
				previewResolveErr = strings.TrimSpace(previewResolveErr)
				if len(previewResolveErr) > 240 {
					previewResolveErr = previewResolveErr[:240] + "..."
				}
			}
			_ = s.repo.UpdateLog(logID, map[string]interface{}{
				"status":      "error",
				"parse_error": func() string {
					if previewResolveErr == "" {
						return "no pdf attachment and no pdf download url found"
					}
					return "no pdf attachment and no pdf download url found; preview link resolve failed: " + previewResolveErr
				}(),
				"invoice_xml_url": func() interface{} {
					if xmlURL == nil {
						return nil
					}
					return *xmlURL
				}(),
			})
			return nil, fmt.Errorf("no pdf attachment and no pdf download url found")
		}
		b, err := downloadURLWithLimitCtx(ctx, *pdfURL, emailParseMaxPDFBytes)
		if err != nil {
			_ = s.repo.UpdateLog(logID, map[string]interface{}{
				"status":      "error",
				"parse_error": fmt.Sprintf("download pdf failed: %v", err),
			})
			return nil, err
		}
		pdfBytes = b
		if pdfFilename == "" {
			pdfFilename = filenameFromURL(*pdfURL, "invoice.pdf")
		}
	}
	if strings.TrimSpace(pdfFilename) == "" {
		pdfFilename = "invoice.pdf"
	}

	savedFilename, relPath, size, sha, err := s.savePDFToUploads(strings.TrimSpace(logRow.OwnerUserID), pdfFilename, pdfBytes)
	if err != nil {
		_ = s.repo.UpdateLog(logID, map[string]interface{}{
			"status":      "error",
			"parse_error": fmt.Sprintf("save pdf failed: %v", err),
		})
		return nil, err
	}

	// Prefer XML for invoice fields/items if available.
	var inv *models.Invoice
	if xmlBytes != nil {
		if extracted, err2 := parseInvoiceXMLToExtracted(xmlBytes); err2 == nil {
			inv, err = s.invoiceService.CreateFromExtracted(strings.TrimSpace(logRow.OwnerUserID), CreateInvoiceInput{
				Filename:     savedFilename,
				OriginalName: pdfFilename,
				FilePath:     relPath,
				FileSize:     size,
				FileSHA256:   sha,
				Source:       "email",
			}, *extracted)
		}
	}
	if inv == nil && xmlURL != nil && strings.TrimSpace(*xmlURL) != "" {
		xmlBytes, err := downloadURLWithLimit(*xmlURL, emailParseMaxXMLBytes)
		if err == nil {
			if extracted, err2 := parseInvoiceXMLToExtracted(xmlBytes); err2 == nil {
				inv, err = s.invoiceService.CreateFromExtracted(strings.TrimSpace(logRow.OwnerUserID), CreateInvoiceInput{
					Filename:     savedFilename,
					OriginalName: pdfFilename,
					FilePath:     relPath,
					FileSize:     size,
					FileSHA256:   sha,
					Source:       "email",
				}, *extracted)
			}
		}
	}

	if inv == nil {
		// Fallback: parse PDF (OCR/PDF extract) if XML is unavailable or failed.
		inv, err = s.invoiceService.Create(strings.TrimSpace(logRow.OwnerUserID), CreateInvoiceInput{
			Filename:     savedFilename,
			OriginalName: pdfFilename,
			FilePath:     relPath,
			FileSize:     size,
			FileSHA256:   sha,
			Source:       "email",
		})
		if err != nil {
			_ = s.repo.UpdateLog(logID, map[string]interface{}{
				"status":      "error",
				"parse_error": fmt.Sprintf("create invoice failed: %v", err),
			})
			return nil, err
		}
	}

	// Save extra itinerary PDFs (optional).
	if len(itineraryPDFs) > 0 && s.invoiceService != nil && inv != nil {
		for _, a := range itineraryPDFs {
			name := strings.TrimSpace(a.Filename)
			if name == "" {
				name = "itinerary.pdf"
			}
			saved, p, sz, sh, err2 := s.savePDFToUploads(strings.TrimSpace(logRow.OwnerUserID), name, a.Bytes)
			if err2 != nil {
				continue
			}
			_, _ = s.invoiceService.CreateAttachmentCtx(ctx, strings.TrimSpace(logRow.OwnerUserID), inv.ID, CreateInvoiceAttachmentInput{
				Kind:         "itinerary",
				Filename:     saved,
				OriginalName: name,
				FilePath:     p,
				FileSize:     &sz,
				FileSHA256:   sh,
				Source:       "email",
			})
		}
	}

	_ = s.repo.UpdateLog(logID, map[string]interface{}{
		"status":            "parsed",
		"parse_error":       nil,
		"parsed_invoice_id": inv.ID,
		"invoice_xml_url": func() interface{} {
			if xmlURL == nil {
				return nil
			}
			return *xmlURL
		}(),
		"invoice_pdf_url": func() interface{} {
			if pdfURL == nil {
				return nil
			}
			return *pdfURL
		}(),
	})

	return inv, nil
}

func (s *EmailService) savePDFToUploads(ownerUserID string, originalName string, content []byte) (filename string, relPath string, size int64, shaHex *string, err error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	name := strings.TrimSpace(originalName)
	if name == "" {
		name = "invoice.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	filename = fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(name))

	targetDir := s.uploadsDir
	if ownerUserID != "" {
		targetDir = filepath.Join(s.uploadsDir, ownerUserID)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", "", 0, nil, err
	}
	abs := filepath.Join(targetDir, filename)
	if err := os.WriteFile(abs, content, 0644); err != nil {
		return "", "", 0, nil, err
	}

	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	size = int64(len(content))
	if ownerUserID != "" {
		relPath = "uploads/" + ownerUserID + "/" + filename
	} else {
		relPath = "uploads/" + filename
	}
	shaHex = &sha
	return filename, relPath, size, shaHex, nil
}

func readWithLimit(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("invalid limit")
	}
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("payload too large")
	}
	return b, nil
}

func downloadURLWithLimit(rawURL string, limit int64) ([]byte, error) {
	return downloadURLWithLimitCtx(context.Background(), rawURL, limit)
}

func downloadURLWithLimitCtx(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing host")
	}

	if err := ensurePublicHost(u.Hostname()); err != nil {
		return nil, err
	}

	release, err := AcquireEmailDownload(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("stopped after 3 redirects")
			}
			return ensurePublicHost(req.URL.Hostname())
		},
	}

	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "smart-bill-manager/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return readWithLimit(resp.Body, limit)
}

func ensurePublicHost(host string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if host == "localhost" {
		return fmt.Errorf("blocked host")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("blocked private ip target: %s", ip.String())
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10:
			return true
		case v4[0] == 127:
			return true
		case v4[0] == 169 && v4[1] == 254:
			return true
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return true
		case v4[0] == 192 && v4[1] == 168:
			return true
		default:
			return false
		}
	}
	// Unique local addresses fc00::/7
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

func filenameFromURL(rawURL string, fallback string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fallback
	}
	base := path.Base(u.Path)
	base = strings.TrimSpace(base)
	if base == "." || base == "/" || base == "" {
		return fallback
	}
	return base
}

var xmlDateRegex = regexp.MustCompile(`(\d{4})\D+(\d{1,2})\D+(\d{1,2})`)

func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) >= 10 && (s[4] == '-' || s[4] == '/') {
		sep := s[4]
		if s[7] == sep {
			parts := strings.Split(s[:10], string(sep))
			if len(parts) == 3 {
				y, _ := strconv.Atoi(parts[0])
				m, _ := strconv.Atoi(parts[1])
				d, _ := strconv.Atoi(parts[2])
				if y >= 2000 && m >= 1 && m <= 12 && d >= 1 && d <= 31 {
					return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
				}
			}
		}
	}
	if m := xmlDateRegex.FindStringSubmatch(s); len(m) == 4 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		da, _ := strconv.Atoi(m[3])
		if y >= 2000 && mo >= 1 && mo <= 12 && da >= 1 && da <= 31 {
			return fmt.Sprintf("%04d-%02d-%02d", y, mo, da)
		}
	}
	return ""
}

func parseInvoiceXMLToExtracted(xmlBytes []byte) (*InvoiceExtractedData, error) {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	dec.CharsetReader = charset.NewReaderLabel

	values := map[string][]string{}
	var current string

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			current = strings.ToLower(strings.TrimSpace(t.Name.Local))
		case xml.EndElement:
			current = ""
		case xml.CharData:
			if current == "" {
				continue
			}
			v := strings.TrimSpace(string(t))
			if v == "" {
				continue
			}
			values[current] = append(values[current], v)
		}
	}

	first := func(keys ...string) string {
		for _, k := range keys {
			k = strings.ToLower(k)
			for _, v := range values[k] {
				v = strings.TrimSpace(v)
				if v != "" {
					return v
				}
			}
		}
		return ""
	}

	invNo := first("fphm", "invoice_number", "invoiceno", "invoicenumber", "fpno", "eiid", "invoiceid", "invoicenum")
	invDate := first("kprq", "invoice_date", "invoicedate", "issuetime", "requesttime", "date")
	seller := first("xfmc", "seller_name", "sellername", "xfname", "seller", "sellername")
	buyer := first("gfmc", "buyer_name", "buyername", "gfname", "buyer", "buyername")

	// Prefer tax-included total (价税合计) when available.
	amountStr := first("totaltax-includedamount", "totaltaxincludedamount", "jshj", "total", "total_amount", "totalamount", "amount", "je", "amt", "hjje")
	taxStr := first("hjse", "totaltaxam", "comtaxam", "tax_amount", "taxamount", "se", "tax")
	totalStr := first("jshj", "totaltax-includedamount", "totaltaxincludedamount", "total", "total_amount", "totalamount")

	var amount *float64
	if amountStr == "" && totalStr != "" {
		amountStr = totalStr
	}
	if v := parseAmountLoose(amountStr); v != nil {
		amount = v
	}
	var taxAmount *float64
	if v := parseAmountLoose(taxStr); v != nil {
		taxAmount = v
	}

	dateNorm := normalizeDate(invDate)
	if dateNorm != "" {
		invDate = dateNorm
	}

	items := buildInvoiceItems(values)

	extracted := &InvoiceExtractedData{
		InvoiceNumber:           ptrString(invNo),
		InvoiceNumberSource:     "xml",
		InvoiceNumberConfidence: 1,
		InvoiceDate:             ptrString(invDate),
		InvoiceDateSource:       "xml",
		InvoiceDateConfidence:   1,
		Amount:                  amount,
		AmountSource:            "xml",
		AmountConfidence:        1,
		TaxAmount:               taxAmount,
		TaxAmountSource:         "xml",
		TaxAmountConfidence:     1,
		SellerName:              ptrString(seller),
		SellerNameSource:        "xml",
		SellerNameConfidence:    1,
		BuyerName:               ptrString(buyer),
		BuyerNameSource:         "xml",
		BuyerNameConfidence:     1,
		Items:                   items,
		RawText:                 "",
	}

	if extracted.InvoiceNumber == nil && extracted.InvoiceDate == nil && extracted.Amount == nil && len(extracted.Items) == 0 {
		return nil, fmt.Errorf("no invoice fields found in xml")
	}
	return extracted, nil
}

func parseAmountLoose(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "￥", "")
	s = strings.ReplaceAll(s, "¥", "")
	s = strings.ReplaceAll(s, "CNY", "")
	s = strings.ReplaceAll(s, "cny", "")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "￥", "")
	s = strings.ReplaceAll(s, "¥", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

func buildInvoiceItems(values map[string][]string) []InvoiceLineItem {
	get := func(k string) []string {
		if values == nil {
			return nil
		}
		return values[strings.ToLower(k)]
	}

	names := get("spmc")
	if len(names) == 0 {
		names = get("xmmc")
	}
	// EInvoice XML (IssuItemInformation/ItemName).
	if len(names) == 0 {
		names = get("itemname")
	}
	specs := get("ggxh")
	if len(specs) == 0 {
		specs = get("specmod")
	}
	units := get("dw")
	if len(units) == 0 {
		units = get("meaunits")
	}
	qtys := get("spsl")
	if len(qtys) == 0 {
		qtys = get("quantity")
	}

	n := len(names)
	if n == 0 {
		return nil
	}
	items := make([]InvoiceLineItem, 0, n)
	for i := 0; i < n; i++ {
		name := strings.TrimSpace(names[i])
		if name == "" {
			continue
		}
		item := InvoiceLineItem{Name: name}
		if i < len(specs) {
			item.Spec = strings.TrimSpace(specs[i])
		}
		if i < len(units) {
			item.Unit = strings.TrimSpace(units[i])
		}
		if i < len(qtys) {
			if q := parseAmountLoose(qtys[i]); q != nil {
				item.Quantity = q
			}
		}
		items = append(items, item)
	}
	return items
}

func firstURLFromText(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	urlRe := regexp.MustCompile(`(?i)(https?://[^\s<>"'()]+|//[^\s<>"'()]+)`)
	all := urlRe.FindAllString(body, -1)
	if len(all) == 0 {
		return ""
	}
	clean := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimRight(s, ">)].,;\"'")
		return s
	}
	for _, raw := range all {
		u := clean(raw)
		if strings.HasPrefix(u, "//") {
			u = "https:" + u
		}
		if u == "" {
			continue
		}
		pu, err := url.Parse(u)
		if err == nil && pu != nil {
			if v := strings.TrimSpace(pu.Query().Get("content")); v != "" {
				v = strings.TrimSpace(strings.TrimRight(v, ">)].,;\"'"))
				if strings.HasPrefix(v, "//") {
					v = "https:" + v
				}
				if strings.HasPrefix(strings.ToLower(v), "http://") || strings.HasPrefix(strings.ToLower(v), "https://") {
					return v
				}
			}
		}
		return u
	}
	return ""
}

func bestPreviewURLFromText(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	urlRe := regexp.MustCompile(`(?i)(https?://[^\s<>"'()]+|//[^\s<>"'()]+)`)
	all := urlRe.FindAllString(body, -1)
	if len(all) == 0 {
		return ""
	}

	clean := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimRight(s, ">)].,;\"'")
		return s
	}
	isAssetURL := func(u string) bool {
		l := strings.ToLower(u)
		switch {
		case strings.Contains(l, ".png"),
			strings.Contains(l, ".jpg"),
			strings.Contains(l, ".jpeg"),
			strings.Contains(l, ".gif"),
			strings.Contains(l, ".webp"),
			strings.Contains(l, ".bmp"),
			strings.Contains(l, ".svg"),
			strings.Contains(l, ".css"),
			strings.Contains(l, ".js"),
			strings.Contains(l, ".woff"),
			strings.Contains(l, ".woff2"),
			strings.Contains(l, ".ttf"):
			return true
		default:
			return false
		}
	}

	expandEmbeddedURLs := func(rawURLs []string) []string {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(rawURLs))
		add := func(s string) {
			s = strings.TrimSpace(s)
			if s == "" {
				return
			}
			if strings.HasPrefix(s, "//") {
				s = "https:" + s
			}
			if _, ok := seen[strings.ToLower(s)]; ok {
				return
			}
			seen[strings.ToLower(s)] = struct{}{}
			out = append(out, s)
		}

		for _, raw := range rawURLs {
			uRaw := clean(raw)
			if uRaw == "" {
				continue
			}
			add(uRaw)

			u := uRaw
			if strings.HasPrefix(u, "//") {
				u = "https:" + u
			}
			pu, err := url.Parse(u)
			if err != nil || pu == nil {
				continue
			}

			// Some providers embed the actual invoice link inside a query parameter (e.g. QR code images with ?content=https://...).
			q := pu.Query()
			for _, key := range []string{"content", "url", "redirect", "target"} {
				v := strings.TrimSpace(q.Get(key))
				if v == "" {
					continue
				}
				v = strings.TrimSpace(strings.TrimRight(v, ">)].,;\"'"))
				add(v)
			}
		}

		return out
	}

	all = expandEmbeddedURLs(all)

	best := ""
	bestScore := -1 << 30

	for _, raw := range all {
		uRaw := clean(raw)
		if uRaw == "" {
			continue
		}
		if strings.HasPrefix(uRaw, "//") {
			uRaw = "https:" + uRaw
		}
		u, err := url.Parse(uRaw)
		if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}

		l := strings.ToLower(uRaw)
		score := 0

		// De-prioritize assets/tracking links.
		if isAssetURL(l) {
			score -= 1000
		}

		// Prefer known providers.
		host := strings.ToLower(strings.TrimSpace(u.Hostname()))
		switch host {
		case "pis.baiwang.com":
			score += 500
		case "u.baiwang.com":
			score += 450
		case "nnfp.jss.com.cn":
			score += 400
		case "of1.cn":
			score += 390
		case "fp.nuonuo.com":
			score += 300
		case "inv.jss.com.cn", "storage.nuonuo.com":
			// Common direct download hosts returned by NuoNuo APIs.
			score += 220
		}

		if isTrackingRedirectHost(host) {
			score -= 2000
		}

		// Nuonuo has many unrelated product portals in email footers; strongly de-prioritize them.
		if strings.HasSuffix(host, ".nuonuo.com") && host != "fp.nuonuo.com" {
			score -= 700
		}
		switch host {
		case "nst.nuonuo.com", "ntf.nuonuo.com", "bmjc.nuonuo.com", "baoxiao.nuonuo.com", "www.nuonuo.com":
			score -= 900
		}

		// Prefer provider-specific preview pages.
		if strings.Contains(l, "previewinvoiceallele") {
			score += 250
		}
		if strings.Contains(l, "/scan-invoice/printqrcode") && strings.Contains(l, "paramlist=") {
			score += 250
		}
		if host == "fp.nuonuo.com" {
			fragLower := strings.ToLower(strings.TrimSpace(u.Fragment))
			if fragLower == "" || fragLower == "/" {
				// Generic Nuonuo portal link, typically not resolvable.
				score -= 600
			}
			if strings.Contains(fragLower, "paramlist=") {
				score += 250
			}
			if strings.Contains(fragLower, "printqrcode") {
				score += 250
			}
		}

		// Prefer direct links.
		if strings.Contains(l, ".pdf") || strings.Contains(l, "formattype=pdf") {
			score += 900
		}
		if strings.Contains(l, ".xml") || strings.Contains(l, "formattype=xml") {
			score += 850
		}

		// Prefer likely identifier-bearing URLs.
		if strings.Contains(l, "param=") {
			score += 120
		}
		if strings.Contains(l, "paramlist=") {
			score += 120
		}

		// De-prioritize generic landing pages.
		if strings.Contains(l, "/scan-invoice/invoiceshow") {
			score -= 200
		}

		if score > bestScore || (score == bestScore && best != "" && len(uRaw) < len(best)) {
			bestScore = score
			best = uRaw
		}
	}

	return best
}

func bestInvoicePreviewURLFromBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	if strings.Contains(strings.ToLower(body), "<a") {
		if u := bestInvoicePreviewURLFromHTML(body); u != "" {
			return u
		}
	}

	// Plain-text fallback: look for the URL that appears right after a CTA label.
	labels := []string{
		"点击链接查看发票",
		"点击链接查看",
		"下载发票",
		"查看发票",
		"领取发票",
	}
	for _, label := range labels {
		if idx := strings.Index(body, label); idx >= 0 {
			window := body[idx:]
			if len(window) > 1200 {
				window = window[:1200]
			}
			if u := firstURLFromText(window); u != "" {
				return u
			}
		}
	}

	return ""
}

func bestInvoicePreviewURLFromHTML(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	ctx := &html.Node{Type: html.ElementNode, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(body), ctx)
	var root *html.Node
	if err == nil && len(nodes) > 0 {
		root = &html.Node{Type: html.ElementNode, Data: "body"}
		for _, n := range nodes {
			root.AppendChild(n)
		}
	} else {
		// Best-effort fallback for malformed fragments.
		doc, err2 := html.Parse(strings.NewReader("<html><body>" + body + "</body></html>"))
		if err2 != nil {
			return ""
		}
		root = doc
	}

	type token struct {
		kind string // "text" | "a"
		text string
		href string
	}
	tokens := make([]token, 0, 256)

	var nodeText func(n *html.Node) string
	nodeText = func(n *html.Node) string {
		if n == nil {
			return ""
		}
		if n.Type == html.TextNode {
			return n.Data
		}
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(nodeText(c))
		}
		return b.String()
	}

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if n.Type == html.TextNode {
			s := strings.TrimSpace(n.Data)
			if s != "" {
				tokens = append(tokens, token{kind: "text", text: s})
			}
		}
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "a") {
			href := ""
			for _, a := range n.Attr {
				if strings.EqualFold(strings.TrimSpace(a.Key), "href") {
					href = strings.TrimSpace(a.Val)
					break
				}
			}
			anchorText := strings.TrimSpace(nodeText(n))
			if href != "" || anchorText != "" {
				tokens = append(tokens, token{kind: "a", text: anchorText, href: href})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	labels := []string{
		"点击链接查看发票",
		"点击链接查看",
		"下载发票",
		"查看发票",
		"领取发票",
	}
	isTrackingURL := func(u string) bool {
		pu, err := url.Parse(strings.TrimSpace(u))
		if err != nil || pu == nil {
			return false
		}
		return isTrackingRedirectHost(pu.Hostname())
	}
	containsAny := func(s string, needles []string) bool {
		for _, n := range needles {
			if strings.Contains(s, n) {
				return true
			}
		}
		return false
	}
	cleanHref := func(href string) string {
		href = strings.TrimSpace(href)
		if href == "" {
			return ""
		}
		if strings.HasPrefix(href, "//") {
			href = "https:" + href
		}
		return href
	}

	// Prefer anchors whose own visible text is the CTA (e.g. "下载发票") or an URL.
	bestTrackingCTA := ""
	for _, tok := range tokens {
		if tok.kind != "a" {
			continue
		}
		if containsAny(tok.text, labels) {
			if u := cleanHref(tok.href); u != "" {
				// Some emails wrap the real invoice link behind a tracking redirect. Prefer returning a
				// direct/known-provider link if one exists later in the body.
				if isTrackingURL(u) {
					if bestTrackingCTA == "" {
						bestTrackingCTA = u
					}
				} else {
					return u
				}
			}
		}
		if u := firstURLFromText(tok.text); u != "" {
			return u
		}
	}

	if bestTrackingCTA != "" {
		return bestTrackingCTA
	}

	// Prefer the first <a href> that follows a CTA label in nearby text.
	for i := 0; i < len(tokens); i++ {
		if tokens[i].kind != "text" {
			continue
		}
		if !containsAny(tokens[i].text, labels) {
			continue
		}
		for j := i + 1; j < len(tokens) && j <= i+12; j++ {
			if tokens[j].kind == "a" {
				if u := cleanHref(tokens[j].href); u != "" {
					return u
				}
				if u := firstURLFromText(tokens[j].text); u != "" {
					return u
				}
			}
			if tokens[j].kind == "text" {
				if u := firstURLFromText(tokens[j].text); u != "" {
					return u
				}
			}
		}
	}

	return ""
}

func isTrackingRedirectHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	// Seen in NuoNuo email templates as a tracking redirect wrapper.
	if host == "linktrace.triggerdelivery.com" || strings.HasSuffix(host, ".triggerdelivery.com") {
		return true
	}
	return false
}

func isDirectInvoicePDFURL(u string) bool {
	l := strings.ToLower(strings.TrimSpace(u))
	if l == "" {
		return false
	}
	if strings.Contains(l, ".pdf") {
		return true
	}
	// Baiwang download endpoint: .../downloadFormat?...&formatType=PDF
	if strings.Contains(l, "formattype=pdf") {
		return true
	}
	return false
}

func isDirectInvoiceXMLURL(u string) bool {
	l := strings.ToLower(strings.TrimSpace(u))
	if l == "" {
		return false
	}
	if strings.Contains(l, ".xml") {
		return true
	}
	// Baiwang download endpoint: .../downloadFormat?...&formatType=XML
	if strings.Contains(l, "formattype=xml") {
		return true
	}
	// Some providers ship XML inside a zip, typically with a /xml/ path segment.
	if strings.Contains(l, ".zip") && strings.Contains(l, "/xml/") {
		return true
	}
	return false
}

func isBadEmailPreviewURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" {
		return true
	}
	l := strings.ToLower(u)

	// Ignore obvious asset links.
	switch {
	case strings.Contains(l, ".png"),
		strings.Contains(l, ".jpg"),
		strings.Contains(l, ".jpeg"),
		strings.Contains(l, ".gif"),
		strings.Contains(l, ".webp"),
		strings.Contains(l, ".svg"),
		strings.Contains(l, ".css"),
		strings.Contains(l, ".js"),
		strings.Contains(l, ".woff"),
		strings.Contains(l, ".woff2"),
		strings.Contains(l, ".ttf"):
		return true
	}

	pu, err := url.Parse(u)
	if err != nil || pu == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(pu.Hostname()))
	pathLower := strings.ToLower(strings.TrimSpace(pu.Path))

	// NuoNuo generic landing page (not invoice-specific).
	if host == "nnfp.jss.com.cn" && strings.HasPrefix(pathLower, "/scan-invoice/invoiceshow") {
		return true
	}
	if isNuonuoPortalRootURL(pu) {
		return true
	}
	if isNuonuoNonInvoicePortalRootURL(pu) {
		return true
	}

	return false
}

func isNuonuoPortalRootURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host != "fp.nuonuo.com" {
		return false
	}
	if strings.TrimSpace(u.RawQuery) != "" {
		return false
	}
	path := strings.TrimSpace(u.Path)
	if path != "" && path != "/" {
		return false
	}
	frag := strings.TrimSpace(u.Fragment)
	return frag == "" || frag == "/"
}

func isNuonuoNonInvoicePortalRootURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if !strings.HasSuffix(host, ".nuonuo.com") {
		return false
	}
	// fp.nuonuo.com is handled separately (it can embed invoice params in the fragment).
	if host == "fp.nuonuo.com" {
		return false
	}
	if strings.TrimSpace(u.RawQuery) != "" {
		return false
	}
	path := strings.TrimSpace(u.Path)
	if path != "" && path != "/" {
		return false
	}
	frag := strings.TrimSpace(u.Fragment)
	return frag == "" || frag == "/"
}

type fetchedURL struct {
	FinalURL     string
	ContentType  string
	ResponseBody []byte
}

const nuonuoBrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func fetchURLWithLimitCtx(ctx context.Context, rawURL string, limit int64) (*fetchedURL, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing host")
	}
	if err := ensurePublicHost(u.Hostname()); err != nil {
		return nil, err
	}

	release, err := AcquireEmailDownload(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("stopped after 3 redirects")
			}
			return ensurePublicHost(req.URL.Hostname())
		},
	}

	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "smart-bill-manager/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := readWithLimit(resp.Body, limit)
	if err != nil {
		return nil, err
	}

	finalURL := u.String()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &fetchedURL{
		FinalURL:     finalURL,
		ContentType:  strings.TrimSpace(resp.Header.Get("Content-Type")),
		ResponseBody: body,
	}, nil
}

func fetchURLWithLimitCtxWithUAAndRedirects(ctx context.Context, rawURL string, limit int64, userAgent string, maxRedirects int) (*fetchedURL, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing host")
	}
	if err := ensurePublicHost(u.Hostname()); err != nil {
		return nil, err
	}
	if maxRedirects <= 0 {
		maxRedirects = 3
	}
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		userAgent = "smart-bill-manager/1.0"
	}

	release, err := AcquireEmailDownload(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return ensurePublicHost(req.URL.Hostname())
		},
	}

	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := readWithLimit(resp.Body, limit)
	if err != nil {
		return nil, err
	}

	finalURL := u.String()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &fetchedURL{
		FinalURL:     finalURL,
		ContentType:  strings.TrimSpace(resp.Header.Get("Content-Type")),
		ResponseBody: body,
	}, nil
}

func resolveInvoiceDownloadLinksFromPreviewURLCtx(ctx context.Context, previewURL string) (xmlURL *string, pdfURL *string, err error) {
	previewURL = strings.TrimSpace(previewURL)
	if previewURL == "" {
		return nil, nil, fmt.Errorf("empty preview url")
	}

	if u, err2 := url.Parse(previewURL); err2 == nil && isNuonuoPortalRootURL(u) {
		return nil, nil, fmt.Errorf("nuonuo link is a generic portal page (missing invoice token)")
	}

	// Provider-specific fast path (e.g. Baiwang preview pages have deterministic download endpoints).
	if x, p, ok := resolveKnownProviderInvoiceLinksCtx(ctx, previewURL); ok {
		return x, p, nil
	}

	// Some providers wrap invoice links in tracking redirects that require more hops than our default fetch helper.
	if u, err2 := url.Parse(previewURL); err2 == nil && u != nil && isTrackingRedirectHost(u.Hostname()) {
		if f, err := fetchURLWithLimitCtxWithUAAndRedirects(ctx, previewURL, emailParseMaxPageBytes, nuonuoBrowserUA, 10); err == nil && f != nil {
			// Retry known-provider detection on the resolved final URL before falling back to scraping.
			if x, p, ok := resolveKnownProviderInvoiceLinksCtx(ctx, f.FinalURL); ok {
				return x, p, nil
			}
			previewURL = strings.TrimSpace(f.FinalURL)
		}
	}

	f, err := fetchURLWithLimitCtx(ctx, previewURL, emailParseMaxPageBytes)
	if err != nil {
		return nil, nil, err
	}

	ct := strings.ToLower(f.ContentType)
	if strings.Contains(ct, "application/pdf") {
		v := strings.TrimSpace(f.FinalURL)
		return nil, &v, nil
	}
	if strings.Contains(ct, "xml") {
		v := strings.TrimSpace(f.FinalURL)
		return &v, nil, nil
	}

	// If the fetched page is a known provider preview, try a provider-specific resolver again using the
	// final URL (some short links resolve only after a HEAD/redirect trick).
	if x, p, ok := resolveKnownProviderInvoiceLinksCtx(ctx, f.FinalURL); ok {
		return x, p, nil
	}

	base, _ := url.Parse(f.FinalURL)
	content := string(f.ResponseBody)

	absURLRe := regexp.MustCompile(`https?://[^\s<>"'()]+`)
	candidates := absURLRe.FindAllString(content, -1)

	relHrefRe := regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)
	for _, m := range relHrefRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		candidates = append(candidates, m[1])
	}

	clean := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimRight(s, ">)].,;\"'")
		return s
	}
	resolve := func(raw string) string {
		raw = clean(raw)
		if raw == "" {
			return ""
		}
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		if !u.IsAbs() && base != nil {
			u = base.ResolveReference(u)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return ""
		}
		return u.String()
	}

	bestPDF := ""
	bestXML := ""

	for _, raw := range candidates {
		u := resolve(raw)
		if u == "" {
			continue
		}
		l := strings.ToLower(u)
		if bestPDF == "" && strings.Contains(l, ".pdf") {
			bestPDF = u
		}
		if bestXML == "" && strings.Contains(l, ".xml") {
			bestXML = u
		}
		if bestPDF != "" && bestXML != "" {
			break
		}
	}

	if bestPDF != "" {
		v := bestPDF
		pdfURL = &v
	}
	if bestXML != "" {
		v := bestXML
		xmlURL = &v
	}

	if pdfURL == nil && xmlURL == nil {
		return nil, nil, fmt.Errorf("no direct pdf/xml links found from preview page")
	}
	return xmlURL, pdfURL, nil
}

func resolveKnownProviderInvoiceLinksCtx(ctx context.Context, previewURL string) (xmlURL *string, pdfURL *string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(previewURL))
	if err != nil || u == nil || u.Scheme == "" || u.Host == "" {
		return nil, nil, false
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))

	// Baiwang (百望云) short links: http(s)://u.baiwang.com/kXXXXX
	// A HEAD request returns the final preview URL containing the "param=" token:
	// https://pis.baiwang.com/smkp-vue/previewInvoiceAllEle?param=...
	if host == "u.baiwang.com" && strings.HasPrefix(strings.TrimLeft(u.Path, "/"), "k") {
		if finalURL, err := followHEADRedirectsCtx(ctx, u.String(), 2); err == nil && finalURL != "" {
			u2, err2 := url.Parse(finalURL)
			if err2 == nil && u2 != nil {
				u = u2
				host = strings.ToLower(strings.TrimSpace(u.Hostname()))
			}
		}
	}

	// Baiwang invoice preview: /smkp-vue/previewInvoiceAllEle?param=...
	if host == "pis.baiwang.com" && strings.HasPrefix(strings.ToLower(u.Path), "/smkp-vue/previewinvoiceallele") {
		param := strings.TrimSpace(u.Query().Get("param"))
		if param == "" {
			return nil, nil, false
		}

		// The preview page's "下载PDF/XML/OFD文件" buttons point to:
		// /bwmg/mix/bw/downloadFormat?param=<param>&formatType=PDF|XML|OFD
		base := &url.URL{Scheme: u.Scheme, Host: u.Host}
		mk := func(format string) *string {
			v := url.Values{}
			v.Set("param", param)
			v.Set("formatType", format)
			rel := &url.URL{Path: "/bwmg/mix/bw/downloadFormat", RawQuery: v.Encode()}
			out := base.ResolveReference(rel).String()
			out = strings.TrimSpace(out)
			if out == "" {
				return nil
			}
			return &out
		}

		return mk("XML"), mk("PDF"), true
	}

	// NuoNuo (诺诺网) invoice links.
	// Their emails often contain a short link (e.g. https://of1.cn/xxxxx or https://nnfp.jss.com.cn/xxxxx) that redirects
	// to a SPA page /scan-invoice/printQrcode?paramList=... which then POSTs to /scan2/getIvcDetailShow.do to retrieve
	// direct PDF/XML URLs.
	if host == "nnfp.jss.com.cn" || host == "of1.cn" {
		finalURL := strings.TrimSpace(u.String())
		if !strings.Contains(finalURL, "paramList=") || !strings.Contains(strings.ToLower(finalURL), "/scan-invoice/printqrcode") {
			f, err := fetchURLWithLimitCtxWithUAAndRedirects(ctx, finalURL, emailParseMaxPageBytes, nuonuoBrowserUA, 6)
			if err != nil {
				return nil, nil, false
			}
			finalURL = strings.TrimSpace(f.FinalURL)
		}

		x, p, err := resolveNuonuoDirectInvoiceLinksFromPrintURLCtx(ctx, finalURL)
		if err == nil && (x != nil || p != nil) {
			return x, p, true
		}
		return nil, nil, false
	}

	// NuoNuo portal links sometimes embed the scan page in the URL fragment, e.g.:
	// https://fp.nuonuo.com/#/scan-invoice/printQrcode?paramList=...
	// We can extract paramList from the fragment and call the nnfp API directly.
	if host == "fp.nuonuo.com" {
		paramList := strings.TrimSpace(u.Query().Get("paramList"))
		fragmentQuery := url.Values{}
		frag := strings.TrimSpace(u.Fragment)
		if idx := strings.Index(frag, "?"); idx >= 0 && idx+1 < len(frag) {
			if v, err := url.ParseQuery(frag[idx+1:]); err == nil {
				fragmentQuery = v
				if paramList == "" {
					paramList = strings.TrimSpace(v.Get("paramList"))
				}
			}
		}
		if paramList == "" {
			return nil, nil, false
		}

		q := url.Values{}
		q.Set("paramList", paramList)
		for _, k := range []string{"code", "aliView", "shortLinkSource", "isOuterPageReq"} {
			if v := strings.TrimSpace(fragmentQuery.Get(k)); v != "" {
				q.Set(k, v)
			}
		}
		printURL := (&url.URL{Scheme: "https", Host: "nnfp.jss.com.cn", Path: "/scan-invoice/printQrcode", RawQuery: q.Encode()}).String()
		x, p, err := resolveNuonuoDirectInvoiceLinksFromPrintURLCtx(ctx, printURL)
		if err == nil && (x != nil || p != nil) {
			return x, p, true
		}
		return nil, nil, false
	}

	return nil, nil, false
}

func nuonuoBuildIvcDetailRequestFromPrintURL(printURL string) (endpointPath string, form url.Values, err error) {
	u, err := url.Parse(strings.TrimSpace(printURL))
	if err != nil || u == nil || u.Scheme == "" || u.Host == "" {
		return "", nil, fmt.Errorf("invalid url")
	}
	q := u.Query()
	paramList := strings.TrimSpace(q.Get("paramList"))
	if paramList == "" {
		return "", nil, fmt.Errorf("missing paramList")
	}

	isOuterPageReq := strings.EqualFold(strings.TrimSpace(q.Get("isOuterPageReq")), "true")
	if isOuterPageReq {
		endpointPath = "/invoice/scan/IvcDetail.do"
	} else {
		endpointPath = "/scan2/getIvcDetailShow.do"
	}

	form = url.Values{}
	form.Set("paramList", paramList)
	form.Set("code", strings.TrimSpace(q.Get("code")))
	form.Set("aliView", strings.TrimSpace(q.Get("aliView")))
	form.Set("invoiceDetailMiddleUri", strings.TrimSpace(printURL))
	form.Set("shortLinkSource", strings.TrimSpace(q.Get("shortLinkSource")))
	return endpointPath, form, nil
}

func resolveNuonuoDirectInvoiceLinksFromPrintURLCtx(ctx context.Context, printURL string) (xmlURL *string, pdfURL *string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	printURL = strings.TrimSpace(printURL)
	endpointPath, form, err := nuonuoBuildIvcDetailRequestFromPrintURL(printURL)
	if err != nil {
		return nil, nil, err
	}

	u, _ := url.Parse(printURL)
	if u == nil {
		return nil, nil, fmt.Errorf("invalid url")
	}
	if err := ensurePublicHost(u.Hostname()); err != nil {
		return nil, nil, err
	}

	release, err := AcquireEmailDownload(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer release()

	postURL := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: endpointPath}).String()

	client := &http.Client{Timeout: 20 * time.Second}
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodPost, postURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", nuonuoBrowserUA)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", (&url.URL{Scheme: u.Scheme, Host: u.Host}).String())
	req.Header.Set("Referer", printURL)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := readWithLimit(resp.Body, emailParseMaxPageBytes)
	if err != nil {
		return nil, nil, err
	}

	type nuonuoResp struct {
		Status    string `json:"status"`
		Msg       string `json:"msg"`
		RouteView string `json:"routeView"`
		Data      *struct {
			InvoiceSimpleVo *struct {
				URL            string `json:"url"`
				XMLURL         string `json:"xmlUrl"`
				OFDDownloadURL string `json:"ofdDownloadUrl"`
			} `json:"invoiceSimpleVo"`
		} `json:"data"`
	}

	var parsed nuonuoResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(parsed.Status) != "0000" {
		msg := strings.TrimSpace(parsed.Msg)
		if msg == "" {
			msg = "nuonuo response status not ok"
		}
		return nil, nil, fmt.Errorf("%s", msg)
	}
	if parsed.Data == nil || parsed.Data.InvoiceSimpleVo == nil {
		return nil, nil, fmt.Errorf("nuonuo response missing invoiceSimpleVo")
	}

	if v := strings.TrimSpace(parsed.Data.InvoiceSimpleVo.XMLURL); v != "" {
		xmlURL = &v
	}
	if v := strings.TrimSpace(parsed.Data.InvoiceSimpleVo.URL); v != "" {
		pdfURL = &v
	}
	return xmlURL, pdfURL, nil
}

func followHEADRedirectsCtx(ctx context.Context, rawURL string, max int) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty url")
	}
	if max <= 0 {
		return rawURL, nil
	}

	release, err := AcquireEmailDownload(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	client := &http.Client{
		Timeout: 20 * time.Second,
		// Don't automatically follow; we want to inspect Location and apply our own host checks.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	cur := rawURL
	for i := 0; i < max; i++ {
		u, err := url.Parse(strings.TrimSpace(cur))
		if err != nil {
			return "", err
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
		}
		if u.Host == "" {
			return "", fmt.Errorf("missing host")
		}
		if err := ensurePublicHost(u.Hostname()); err != nil {
			return "", err
		}

		rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		req, err := http.NewRequestWithContext(rctx, http.MethodHead, u.String(), nil)
		if err != nil {
			cancel()
			return "", err
		}
		req.Header.Set("User-Agent", "smart-bill-manager/1.0")

		resp, err := client.Do(req)
		cancel()
		if err != nil {
			return "", err
		}
		_ = resp.Body.Close()

		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return cur, nil
		}
		loc := strings.TrimSpace(resp.Header.Get("Location"))
		if loc == "" {
			return cur, nil
		}
		next, err := url.Parse(loc)
		if err != nil {
			return "", err
		}
		if !next.IsAbs() {
			next = u.ResolveReference(next)
		}
		cur = next.String()
	}

	return cur, nil
}
