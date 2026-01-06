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
)

func (s *EmailService) ParseEmailLog(ownerUserID string, logID string) (*models.Invoice, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("missing owner_user_id")
	}

	logID = strings.TrimSpace(logID)
	if logID == "" {
		return nil, fmt.Errorf("missing log id")
	}

	logRow, err := s.repo.FindLogByID(logID)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(logRow.OwnerUserID) != ownerUserID {
		// Avoid leaking other users' log ids.
		return nil, gorm.ErrRecordNotFound
	}

	if logRow.ParsedInvoiceID != nil && strings.TrimSpace(*logRow.ParsedInvoiceID) != "" {
		return s.invoiceService.GetByID(strings.TrimSpace(logRow.OwnerUserID), strings.TrimSpace(*logRow.ParsedInvoiceID))
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

	cfg, err := s.repo.FindConfigByID(logRow.EmailConfigID)
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
		textParts   []string
	)

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = s.repo.UpdateLog(logID, map[string]interface{}{
				"status":      "error",
				"parse_error": fmt.Sprintf("read email part failed: %v", err),
			})
			return nil, err
		}

		switch h := part.Header.(type) {
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			low := strings.ToLower(strings.TrimSpace(filename))
			if pdfBytes == nil && strings.HasSuffix(low, ".pdf") {
				b, err := readWithLimit(part.Body, emailParseMaxPDFBytes)
				if err != nil {
					_ = s.repo.UpdateLog(logID, map[string]interface{}{
						"status":      "error",
						"parse_error": fmt.Sprintf("read pdf attachment failed: %v", err),
					})
					return nil, err
				}
				pdfFilename = filename
				pdfBytes = b
			}
		default:
			// Inline + other body parts: collect for link parsing (xml/pdf download URLs).
			if len(textParts) < 12 {
				if b, err := readWithLimit(part.Body, emailParseMaxTextBytes); err == nil {
					s := strings.TrimSpace(string(b))
					if s != "" {
						textParts = append(textParts, s)
					}
				}
			}
		}
	}

	bodyText := strings.Join(textParts, "\n")
	xmlURL := logRow.InvoiceXMLURL
	pdfURL := logRow.InvoicePDFURL

	foundXML, foundPDF := extractInvoiceLinksFromText(bodyText)
	if xmlURL == nil && foundXML != nil {
		xmlURL = foundXML
	}
	if pdfURL == nil && foundPDF != nil {
		pdfURL = foundPDF
	}

	// Ensure we have the PDF bytes for preview (either from attachment or from a PDF download link).
	if pdfBytes == nil {
		if pdfURL == nil {
			_ = s.repo.UpdateLog(logID, map[string]interface{}{
				"status":      "error",
				"parse_error": "no pdf attachment and no pdf download url found",
				"invoice_xml_url": func() interface{} {
					if xmlURL == nil {
						return nil
					}
					return *xmlURL
				}(),
			})
			return nil, fmt.Errorf("no pdf attachment and no pdf download url found")
		}
		b, err := downloadURLWithLimit(*pdfURL, emailParseMaxPDFBytes)
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
	if xmlURL != nil && strings.TrimSpace(*xmlURL) != "" {
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
				if err == nil {
					_ = s.repo.UpdateLog(logID, map[string]interface{}{
						"status":            "parsed",
						"parse_error":       nil,
						"parsed_invoice_id": inv.ID,
						"invoice_xml_url":   *xmlURL,
						"invoice_pdf_url": func() interface{} {
							if pdfURL == nil {
								return nil
							}
							return *pdfURL
						}(),
					})
					return inv, nil
				}
			}
		}
	}

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

	release, err := AcquireEmailDownload(context.Background())
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

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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

	invNo := first("fphm", "invoice_number", "invoiceno", "invoicenumber", "fpno")
	invDate := first("kprq", "invoice_date", "invoicedate", "date")
	seller := first("xfmc", "seller_name", "sellername", "xfname")
	buyer := first("gfmc", "buyer_name", "buyername", "gfname")
	amountStr := first("hjje", "amount", "je", "amt")
	taxStr := first("hjse", "tax_amount", "taxamount", "se", "tax")
	totalStr := first("jshj", "total", "total_amount", "totalamount")

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
	specs := get("ggxh")
	units := get("dw")
	qtys := get("spsl")

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
