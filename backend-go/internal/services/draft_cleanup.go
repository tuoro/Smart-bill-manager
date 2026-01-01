package services

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

func StartDraftCleanup(db *gorm.DB, uploadsDir string) {
	if db == nil {
		return
	}

	ttlHours := envInt("SBM_DRAFT_TTL_HOURS", 6)
	intervalMinutes := envInt("SBM_DRAFT_CLEANUP_INTERVAL_MINUTES", 15)
	if ttlHours <= 0 || intervalMinutes <= 0 {
		log.Printf("[DraftCleanup] disabled (SBM_DRAFT_TTL_HOURS=%d SBM_DRAFT_CLEANUP_INTERVAL_MINUTES=%d)", ttlHours, intervalMinutes)
		return
	}

	ttl := time.Duration(ttlHours) * time.Hour
	interval := time.Duration(intervalMinutes) * time.Minute

	cleanupOnce := func() {
		cutoff := time.Now().Add(-ttl)
		payDeleted, invDeleted, fileDeleted := cleanupDraftsOnce(db, uploadsDir, cutoff)
		if payDeleted > 0 || invDeleted > 0 || fileDeleted > 0 {
			log.Printf("[DraftCleanup] removed payments=%d invoices=%d files=%d (cutoff=%s)", payDeleted, invDeleted, fileDeleted, cutoff.Format(time.RFC3339))
		}
	}

	cleanupOnce()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupOnce()
		}
	}()
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func cleanupDraftsOnce(db *gorm.DB, uploadsDir string, cutoff time.Time) (paymentsDeleted int, invoicesDeleted int, filesDeleted int) {
	type payRow struct {
		ID             string
		ScreenshotPath *string
	}
	var payRows []payRow
	_ = db.Model(&models.Payment{}).
		Select("id, screenshot_path").
		Where("is_draft = 1 AND created_at < ?", cutoff).
		Scan(&payRows).Error

	type invRow struct {
		ID       string
		FilePath string
	}
	var invRows []invRow
	_ = db.Model(&models.Invoice{}).
		Select("id, file_path").
		Where("is_draft = 1 AND created_at < ?", cutoff).
		Scan(&invRows).Error

	payIDs := make([]string, 0, len(payRows))
	for _, r := range payRows {
		payIDs = append(payIDs, strings.TrimSpace(r.ID))
		if r.ScreenshotPath == nil || strings.TrimSpace(*r.ScreenshotPath) == "" {
			continue
		}
		if removeStoredFile(uploadsDir, strings.TrimSpace(*r.ScreenshotPath)) {
			filesDeleted++
		}
	}

	invIDs := make([]string, 0, len(invRows))
	for _, r := range invRows {
		invIDs = append(invIDs, strings.TrimSpace(r.ID))
		if strings.TrimSpace(r.FilePath) == "" {
			continue
		}
		if removeStoredFile(uploadsDir, strings.TrimSpace(r.FilePath)) {
			filesDeleted++
		}
	}

	_ = db.Transaction(func(tx *gorm.DB) error {
		if len(payIDs) > 0 {
			tx.Where("payment_id IN ?", payIDs).Delete(&models.InvoicePaymentLink{})
			if err := tx.Where("id IN ?", payIDs).Delete(&models.Payment{}).Error; err == nil {
				paymentsDeleted = len(payIDs)
			}
		}
		if len(invIDs) > 0 {
			tx.Where("invoice_id IN ?", invIDs).Delete(&models.InvoicePaymentLink{})
			if err := tx.Where("id IN ?", invIDs).Delete(&models.Invoice{}).Error; err == nil {
				invoicesDeleted = len(invIDs)
			}
		}
		return nil
	})

	return paymentsDeleted, invoicesDeleted, filesDeleted
}

func removeStoredFile(uploadsDir string, storedPath string) bool {
	p := strings.TrimSpace(storedPath)
	if p == "" {
		return false
	}
	abs := resolveUploadsPathAbs(uploadsDir, p)
	if abs == "" {
		return false
	}
	if err := os.Remove(abs); err == nil {
		return true
	}
	return false
}

func resolveUploadsPathAbs(uploadsDir, storedPath string) string {
	uploadsDir = strings.TrimSpace(uploadsDir)
	storedPath = strings.TrimSpace(storedPath)
	if uploadsDir == "" || storedPath == "" {
		return ""
	}

	// Normalize separators for prefix handling.
	p := strings.ReplaceAll(storedPath, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "uploads/")

	cleanRel := filepath.Clean(p)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return ""
	}

	abs := filepath.Join(uploadsDir, cleanRel)
	abs = filepath.Clean(abs)
	return abs
}

