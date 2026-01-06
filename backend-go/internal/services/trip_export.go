package services

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

type tripExportInvoice struct {
	ID            string
	OriginalName  string
	FilePath      string
	InvoiceNumber *string
	InvoiceDate   *string
	SellerName    *string
	CreatedAt     time.Time
}

func (s *TripService) PrepareTripExportZip(ctx context.Context, ownerUserID string, tripID string) (*ZipStream, error) {
	db := database.GetDB()
	ownerUserID = strings.TrimSpace(ownerUserID)
	tripID = strings.TrimSpace(tripID)
	if ownerUserID == "" || tripID == "" {
		return nil, fmt.Errorf("missing owner_user_id or trip_id")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var trip models.Trip
	if err := db.Model(&models.Trip{}).
		Where("id = ? AND owner_user_id = ?", tripID, ownerUserID).
		First(&trip).Error; err != nil {
		return nil, err
	}

	var payments []models.Payment
	if err := db.Model(&models.Payment{}).
		Where("owner_user_id = ?", ownerUserID).
		Where("trip_id = ?", tripID).
		Where("is_draft = 0").
		Order("transaction_time_ts ASC, id ASC").
		Find(&payments).Error; err != nil {
		return nil, err
	}
	if len(payments) == 0 {
		return nil, fmt.Errorf("no payments to export")
	}

	paymentIDs := make([]string, 0, len(payments))
	for _, p := range payments {
		paymentIDs = append(paymentIDs, p.ID)
	}

	type linkRow struct {
		PaymentID string
		InvoiceID string
	}
	var links []linkRow
	if err := db.
		Table("invoice_payment_links").
		Select("payment_id, invoice_id").
		Where("payment_id IN ?", paymentIDs).
		Scan(&links).Error; err != nil {
		return nil, err
	}

	invoiceIDsSet := make(map[string]struct{}, len(links))
	byPayment := make(map[string][]string, len(paymentIDs))
	for _, l := range links {
		invoiceIDsSet[l.InvoiceID] = struct{}{}
		byPayment[l.PaymentID] = append(byPayment[l.PaymentID], l.InvoiceID)
	}

	invoiceIDs := make([]string, 0, len(invoiceIDsSet))
	for id := range invoiceIDsSet {
		invoiceIDs = append(invoiceIDs, id)
	}

	invByID := map[string]tripExportInvoice{}
	if len(invoiceIDs) > 0 {
		var invoices []models.Invoice
		if err := db.Model(&models.Invoice{}).
			Where("owner_user_id = ?", ownerUserID).
			Where("id IN ?", invoiceIDs).
			Where("is_draft = 0").
			Find(&invoices).Error; err != nil {
			return nil, err
		}
		for _, inv := range invoices {
			invByID[inv.ID] = tripExportInvoice{
				ID:            inv.ID,
				OriginalName:  inv.OriginalName,
				FilePath:      inv.FilePath,
				InvoiceNumber: inv.InvoiceNumber,
				InvoiceDate:   inv.InvoiceDate,
				SellerName:    inv.SellerName,
				CreatedAt:     inv.CreatedAt,
			}
		}
	}

	width := len(fmt.Sprintf("%d", len(payments)))
	if width < 3 {
		width = 3
	}

	now := time.Now().Format("20060102_150405")
	zipBase := "trip"
	if strings.TrimSpace(trip.Name) != "" {
		zipBase = "trip_" + sanitizeZipComponent(trip.Name, 40)
	}
	if zipBase == "" {
		zipBase = "trip"
	}

	rootDir := zipBase + "_" + now
	zipName := rootDir + ".zip"

	return &ZipStream{
		Filename: zipName,
		Write: func(w io.Writer) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			zw := zip.NewWriter(w)

			var warnings []string
			_, _ = zw.Create(rootDir + "/")

			for i, p := range payments {
				if err := ctx.Err(); err != nil {
					return err
				}
				seq := fmt.Sprintf("%0*d", width, i+1)
				when := formatZipTimeLabel(p.TransactionTime, p.CreatedAt)
				merchant := sanitizeZipComponent(ptrOrEmpty(p.Merchant), 24)
				amount := sanitizeZipComponent(fmt.Sprintf("%.2f", p.Amount), 16)

				paymentDir := rootDir + "/" + strings.Trim(sanitizeZipComponent(strings.Join([]string{seq, when, merchant, amount}, "_"), 120), "_") + "/"
				_, _ = zw.Create(paymentDir)

				// Payment screenshot (optional)
				if p.ScreenshotPath != nil && strings.TrimSpace(*p.ScreenshotPath) != "" {
					stored := strings.TrimSpace(*p.ScreenshotPath)
					abs, err := resolveUploadsFilePathAbs(s.uploadsDir, stored)
					if err != nil {
						warnings = append(warnings, fmt.Sprintf("payment %s screenshot path invalid: %s (%v)", p.ID, stored, err))
					} else if err := zipAddFile(ctx, zw, paymentDir+("payment_screenshot"+fileExtOrDefault(stored, ".png")), abs); err != nil {
						warnings = append(warnings, fmt.Sprintf("payment %s screenshot read failed: %s (%v)", p.ID, stored, err))
					}
				}

				// Linked invoices (0..N)
				invIDs := byPayment[p.ID]
				invs := make([]tripExportInvoice, 0, len(invIDs))
				for _, invID := range invIDs {
					if inv, ok := invByID[invID]; ok {
						invs = append(invs, inv)
					}
				}
				sort.Slice(invs, func(a, b int) bool {
					da := invoiceDateKey(invs[a].InvoiceDate)
					db := invoiceDateKey(invs[b].InvoiceDate)
					if da != db {
						return da < db
					}
					if !invs[a].CreatedAt.Equal(invs[b].CreatedAt) {
						return invs[a].CreatedAt.Before(invs[b].CreatedAt)
					}
					return invs[a].ID < invs[b].ID
				})

				for j, inv := range invs {
					if err := ctx.Err(); err != nil {
						return err
					}
					sub := indexToLetters(j)
					label := inv.ID
					if inv.InvoiceNumber != nil && strings.TrimSpace(*inv.InvoiceNumber) != "" {
						label = strings.TrimSpace(*inv.InvoiceNumber)
					} else if inv.SellerName != nil && strings.TrimSpace(*inv.SellerName) != "" {
						label = strings.TrimSpace(*inv.SellerName)
					} else if len(inv.ID) >= 8 {
						label = inv.ID[:8]
					}
					label = sanitizeZipComponent(label, 36)

					stored := strings.TrimSpace(inv.FilePath)
					if stored == "" {
						warnings = append(warnings, fmt.Sprintf("invoice %s file_path missing", inv.ID))
						continue
					}

					abs, err := resolveUploadsFilePathAbs(s.uploadsDir, stored)
					if err != nil {
						warnings = append(warnings, fmt.Sprintf("invoice %s path invalid: %s (%v)", inv.ID, stored, err))
						continue
					}

					ext := filepath.Ext(inv.OriginalName)
					if ext == "" {
						ext = fileExtOrDefault(stored, ".pdf")
					}
					name := fmt.Sprintf("invoice_%s_%s%s", sub, label, ext)
					if err := zipAddFile(ctx, zw, paymentDir+name, abs); err != nil {
						warnings = append(warnings, fmt.Sprintf("invoice %s read failed: %s (%v)", inv.ID, stored, err))
					}
				}
			}

			if len(warnings) > 0 {
				b := []byte(strings.Join(warnings, "\n") + "\n")
				if f, err := zw.Create(rootDir + "/WARNINGS.txt"); err == nil {
					_, _ = f.Write(b)
				}
			}

			return zw.Close()
		},
	}, nil
}

func zipAddFile(ctx context.Context, zw *zip.Writer, zipPath string, absPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	zipPath = strings.ReplaceAll(zipPath, "\\", "/")
	zipPath = strings.TrimPrefix(zipPath, "/")
	if zipPath == "" || strings.Contains(zipPath, "..") {
		return fmt.Errorf("invalid zip path")
	}

	fw, err := zw.Create(zipPath)
	if err != nil {
		return err
	}
	r, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer r.Close()
	buf := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, werr := fw.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

func fileExtOrDefault(path string, def string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return def
	}
	return ext
}

func formatZipTimeLabel(rfc3339 string, fallback time.Time) string {
	rfc3339 = strings.TrimSpace(rfc3339)
	if rfc3339 != "" {
		if t, err := time.Parse(time.RFC3339, rfc3339); err == nil {
			return t.Format("20060102_150405")
		}
	}
	if !fallback.IsZero() {
		return fallback.Format("20060102_150405")
	}
	return "unknown_time"
}

var zipComponentUnsafe = regexp.MustCompile(`[^\pL\pN._()\-]+`)

func sanitizeZipComponent(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = zipComponentUnsafe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._- ")
	s = strings.TrimSpace(s)
	s = regexp.MustCompile(`_+`).ReplaceAllString(s, "_")
	if maxLen > 0 && len([]rune(s)) > maxLen {
		r := []rune(s)
		s = string(r[:maxLen])
	}
	return s
}

func ptrOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

var invoiceDateAnyRegex = regexp.MustCompile(`(\d{4})\D+(\d{1,2})\D+(\d{1,2})`)

func invoiceDateKey(s *string) string {
	if s == nil {
		return "9999-99-99"
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return "9999-99-99"
	}
	if len(v) >= 10 && (v[4] == '-' || v[4] == '/') {
		v = strings.ReplaceAll(v[:10], "/", "-")
		return v
	}
	if m := invoiceDateAnyRegex.FindStringSubmatch(v); len(m) == 4 {
		month, err1 := strconv.Atoi(m[2])
		day, err2 := strconv.Atoi(m[3])
		if err1 == nil && err2 == nil && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			return fmt.Sprintf("%s-%02d-%02d", m[1], month, day)
		}
	}
	return "9999-99-99"
}

func indexToLetters(idx int) string {
	n := idx + 1
	out := ""
	for n > 0 {
		n--
		out = string(rune('a'+(n%26))) + out
		n /= 26
	}
	if out == "" {
		return "a"
	}
	return out
}

func resolveUploadsDirAbs(uploadsDir string) (string, error) {
	if uploadsDir == "" {
		uploadsDir = "uploads"
	}
	if filepath.IsAbs(uploadsDir) {
		return filepath.Clean(uploadsDir), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, uploadsDir)), nil
}

// resolveUploadsFilePathAbs resolves an uploads-relative path stored in DB (e.g. "uploads/abc.png")
// to an absolute file path under uploadsDir, preventing path traversal.
func resolveUploadsFilePathAbs(uploadsDir string, storedPath string) (string, error) {
	uploadsDirAbs, err := resolveUploadsDirAbs(uploadsDir)
	if err != nil {
		return "", err
	}
	uploadsDirAbs, err = filepath.Abs(uploadsDirAbs)
	if err != nil {
		return "", err
	}

	p := strings.TrimSpace(storedPath)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}

	p = strings.ReplaceAll(p, "\\", "/")

	if filepath.IsAbs(p) {
		abs := filepath.Clean(p)
		rel, err := filepath.Rel(uploadsDirAbs, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("path escapes uploads dir")
		}
		return abs, nil
	}

	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "uploads/")

	cleanRel := filepath.Clean(p)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid relative path")
	}

	abs := filepath.Join(uploadsDirAbs, cleanRel)
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(uploadsDirAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes uploads dir")
	}
	return abs, nil
}
