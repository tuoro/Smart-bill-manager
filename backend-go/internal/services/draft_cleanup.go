package services

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

func StartDraftCleanup(db *gorm.DB, uploadsDir string) {
	if db == nil {
		return
	}

	lastRun := time.Time{}
	cleanupOnce := func() {
		s, err := GetSystemSettings()
		if err != nil {
			s = defaultSystemSettings()
		}

		if !s.Cleanup.Enabled || s.Cleanup.DraftTTLHours <= 0 {
			return
		}

		ttl := time.Duration(s.Cleanup.DraftTTLHours) * time.Hour
		cutoff := time.Now().Add(-ttl)

		max := s.Cleanup.MaxDeletePerRun
		payDeleted, invDeleted, fileDeleted := cleanupDraftsOnce(db, uploadsDir, cutoff, max)
		orphanDeleted := 0
		if s.Cleanup.OrphanFileTTLHours > 0 {
			orphanCutoff := time.Now().Add(-time.Duration(s.Cleanup.OrphanFileTTLHours) * time.Hour)
			orphanDeleted = cleanupOrphanFilesOnce(db, uploadsDir, orphanCutoff, max)
		}

		if payDeleted > 0 || invDeleted > 0 || fileDeleted > 0 || orphanDeleted > 0 {
			log.Printf(
				"[DraftCleanup] removed payments=%d invoices=%d files=%d orphans=%d (draft_cutoff=%s)",
				payDeleted,
				invDeleted,
				fileDeleted,
				orphanDeleted,
				cutoff.Format(time.RFC3339),
			)
		}
		lastRun = time.Now()
	}

	// Initial run
	cleanupOnce()

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s, err := GetSystemSettings()
			if err != nil {
				s = defaultSystemSettings()
			}
			if !s.Cleanup.Enabled || s.Cleanup.DraftTTLHours <= 0 || s.Cleanup.IntervalMinutes <= 0 {
				continue
			}
			interval := time.Duration(s.Cleanup.IntervalMinutes) * time.Minute
			if lastRun.IsZero() || time.Since(lastRun) >= interval {
				cleanupOnce()
			}
		}
	}()
}

func cleanupDraftsOnce(db *gorm.DB, uploadsDir string, cutoff time.Time, maxDeletePerRun int) (paymentsDeleted int, invoicesDeleted int, filesDeleted int) {
	type payRow struct {
		ID             string
		ScreenshotPath *string
	}
	var payRows []payRow
	_ = db.Model(&models.Payment{}).
		Select("id, screenshot_path").
		Where("is_draft = 1 AND created_at < ?", cutoff).
		Limit(maxDeletePerRun).
		Scan(&payRows).Error

	type invRow struct {
		ID       string
		FilePath string
	}
	var invRows []invRow
	_ = db.Model(&models.Invoice{}).
		Select("id, file_path").
		Where("is_draft = 1 AND created_at < ?", cutoff).
		Limit(maxDeletePerRun).
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

func cleanupOrphanFilesOnce(db *gorm.DB, uploadsDir string, cutoff time.Time, maxDeletePerRun int) int {
	uploadsDir = strings.TrimSpace(uploadsDir)
	if uploadsDir == "" {
		return 0
	}

	refs := make(map[string]struct{}, 1024)

	type payRef struct {
		ScreenshotPath *string `gorm:"column:screenshot_path"`
	}
	var payRefs []payRef
	_ = db.Model(&models.Payment{}).Select("screenshot_path").Where("screenshot_path IS NOT NULL AND TRIM(screenshot_path) != ''").Scan(&payRefs).Error
	for _, r := range payRefs {
		if r.ScreenshotPath == nil {
			continue
		}
		if p := normalizeStoredUploadsPath(*r.ScreenshotPath); p != "" {
			refs[p] = struct{}{}
		}
	}

	type invRef struct {
		FilePath string `gorm:"column:file_path"`
	}
	var invRefs []invRef
	_ = db.Model(&models.Invoice{}).Select("file_path").Where("file_path IS NOT NULL AND TRIM(file_path) != ''").Scan(&invRefs).Error
	for _, r := range invRefs {
		if p := normalizeStoredUploadsPath(r.FilePath); p != "" {
			refs[p] = struct{}{}
		}
	}

	removed := 0
	_ = filepath.WalkDir(uploadsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if maxDeletePerRun > 0 && removed >= maxDeletePerRun {
			return filepath.SkipAll
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}

		rel, relErr := filepath.Rel(uploadsDir, path)
		if relErr != nil {
			return nil
		}
		stored := normalizeStoredUploadsPath(rel)
		if stored == "" {
			return nil
		}
		if _, ok := refs[stored]; ok {
			return nil
		}

		if rmErr := os.Remove(path); rmErr == nil {
			removed++
		}
		return nil
	})

	return removed
}

func normalizeStoredUploadsPath(storedPath string) string {
	p := strings.TrimSpace(storedPath)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "uploads/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return "uploads/" + p
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
