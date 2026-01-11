package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
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
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html/charset"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

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
		if hl, ok := part.Header.(emailHeaderLike); ok {
			ct, _, _ = hl.ContentType()
			ct = strings.ToLower(strings.TrimSpace(ct))

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
		if len(textParts) < 12 && (ct == "" || strings.HasPrefix(ct, "text/")) {
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

	if logRow.ParsedInvoiceID != nil && strings.TrimSpace(*logRow.ParsedInvoiceID) != "" {
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
	if pdfURL == nil && xmlURL == nil {
		if previewURL := firstURLFromText(bodyText); previewURL != "" {
			resXML, resPDF, err := resolveInvoiceDownloadLinksFromPreviewURLCtx(ctx, previewURL)
			if err == nil {
				if xmlURL == nil && resXML != nil && strings.TrimSpace(*resXML) != "" {
					xmlURL = resXML
				}
				if pdfURL == nil && resPDF != nil && strings.TrimSpace(*resPDF) != "" {
					pdfURL = resPDF
				}
				if (resXML != nil || resPDF != nil) && logID != "" {
					_ = s.repo.UpdateLog(logID, map[string]interface{}{
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
				}
			} else {
				previewResolveErr = err.Error()
			}
		}
	}

	// Ensure we have the PDF bytes for preview (either from attachment or from a PDF download link).
	if pdfBytes == nil {
		if pdfURL == nil {
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
	urlRe := regexp.MustCompile(`https?://[^\s<>"'()]+`)
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
		if u == "" {
			continue
		}
		return u
	}
	return ""
}

type fetchedURL struct {
	FinalURL     string
	ContentType  string
	ResponseBody []byte
}

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

func resolveInvoiceDownloadLinksFromPreviewURLCtx(ctx context.Context, previewURL string) (xmlURL *string, pdfURL *string, err error) {
	previewURL = strings.TrimSpace(previewURL)
	if previewURL == "" {
		return nil, nil, fmt.Errorf("empty preview url")
	}

	// Provider-specific fast path (e.g. Baiwang preview pages have deterministic download endpoints).
	if x, p, ok := resolveKnownProviderInvoiceLinksCtx(ctx, previewURL); ok {
		return x, p, nil
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

	return nil, nil, false
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
