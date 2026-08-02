package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

type paymentDraftCleanupRow struct {
	ID             string
	ScreenshotPath *string
}

type invoiceDraftCleanupRow struct {
	ID       string
	FilePath string
}

type attachmentDraftCleanupRow struct {
	FilePath string
}

func StartDraftCleanup(ctx context.Context, db *gorm.DB, uploadsDir string) <-chan struct{} {
	done := make(chan struct{})
	if db == nil {
		close(done)
		return done
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ttlHours := envInt("SBM_DRAFT_TTL_HOURS", 6)
	intervalMinutes := envInt("SBM_DRAFT_CLEANUP_INTERVAL_MINUTES", 15)
	if ttlHours <= 0 || intervalMinutes <= 0 {
		log.Printf("[DraftCleanup] disabled (SBM_DRAFT_TTL_HOURS=%d SBM_DRAFT_CLEANUP_INTERVAL_MINUTES=%d)", ttlHours, intervalMinutes)
		close(done)
		return done
	}

	ttl := time.Duration(ttlHours) * time.Hour
	interval := time.Duration(intervalMinutes) * time.Minute

	cleanupOnce := func() {
		cutoff := time.Now().Add(-ttl)
		payDeleted, invDeleted, fileDeleted, err := cleanupDraftsOnce(ctx, db, uploadsDir, cutoff)
		if err != nil {
			log.Printf("[DraftCleanup] cleanup failed: %v", err)
		}
		if payDeleted > 0 || invDeleted > 0 || fileDeleted > 0 {
			log.Printf("[DraftCleanup] removed payments=%d invoices=%d files=%d (cutoff=%s)", payDeleted, invDeleted, fileDeleted, cutoff.Format(time.RFC3339))
		}
	}

	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			return
		default:
			cleanupOnce()
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanupOnce()
			}
		}
	}()
	return done
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

func cleanupDraftsOnce(ctx context.Context, db *gorm.DB, uploadsDir string, cutoff time.Time) (paymentsDeleted int, invoicesDeleted int, filesDeleted int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var payRows []paymentDraftCleanupRow
	var invRows []invoiceDraftCleanupRow
	var attachmentRows []attachmentDraftCleanupRow

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Payment{}).
			Select("id, screenshot_path").
			Where("is_draft = 1 AND created_at < ?", cutoff).
			Scan(&payRows).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Invoice{}).
			Select("id, file_path").
			Where("is_draft = 1 AND created_at < ?", cutoff).
			Scan(&invRows).Error; err != nil {
			return err
		}

		payIDs := nonEmptyPaymentDraftIDs(payRows)
		invIDs := nonEmptyInvoiceDraftIDs(invRows)
		if len(payIDs) > 0 {
			if err := tx.Where("payment_id IN ?", payIDs).Delete(&models.InvoicePaymentLink{}).Error; err != nil {
				return err
			}
			if err := tx.Where("payment_id IN ?", payIDs).Delete(&models.PaymentOCRBlob{}).Error; err != nil {
				return err
			}
			res := tx.Where("id IN ? AND is_draft = 1 AND created_at < ?", payIDs, cutoff).Delete(&models.Payment{})
			if res.Error != nil {
				return res.Error
			}
			paymentsDeleted = int(res.RowsAffected)
		}
		if len(invIDs) > 0 {
			if err := tx.Model(&models.InvoiceAttachment{}).
				Select("file_path").
				Where("invoice_id IN ?", invIDs).
				Scan(&attachmentRows).Error; err != nil {
				return err
			}
			if err := tx.Where("invoice_id IN ?", invIDs).Delete(&models.InvoicePaymentLink{}).Error; err != nil {
				return err
			}
			if err := tx.Where("invoice_id IN ?", invIDs).Delete(&models.InvoiceAttachment{}).Error; err != nil {
				return err
			}
			if err := tx.Where("invoice_id IN ?", invIDs).Delete(&models.InvoiceOCRBlob{}).Error; err != nil {
				return err
			}
			res := tx.Where("id IN ? AND is_draft = 1 AND created_at < ?", invIDs, cutoff).Delete(&models.Invoice{})
			if res.Error != nil {
				return res.Error
			}
			invoicesDeleted = int(res.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, 0, 0, err
	}

	var fileErrors []error
	for _, row := range payRows {
		if row.ScreenshotPath == nil || strings.TrimSpace(*row.ScreenshotPath) == "" {
			continue
		}
		deleted, removeErr := removeStoredFile(uploadsDir, *row.ScreenshotPath)
		if deleted {
			filesDeleted++
		}
		if removeErr != nil {
			fileErrors = append(fileErrors, removeErr)
		}
	}
	for _, row := range invRows {
		if strings.TrimSpace(row.FilePath) == "" {
			continue
		}
		deleted, removeErr := removeStoredFile(uploadsDir, row.FilePath)
		if deleted {
			filesDeleted++
		}
		if removeErr != nil {
			fileErrors = append(fileErrors, removeErr)
		}
	}
	for _, row := range attachmentRows {
		if strings.TrimSpace(row.FilePath) == "" {
			continue
		}
		deleted, removeErr := removeStoredFile(uploadsDir, row.FilePath)
		if deleted {
			filesDeleted++
		}
		if removeErr != nil {
			fileErrors = append(fileErrors, removeErr)
		}
	}

	return paymentsDeleted, invoicesDeleted, filesDeleted, errors.Join(fileErrors...)
}

func nonEmptyPaymentDraftIDs(rows []paymentDraftCleanupRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id := strings.TrimSpace(row.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func nonEmptyInvoiceDraftIDs(rows []invoiceDraftCleanupRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id := strings.TrimSpace(row.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func removeStoredFile(uploadsDir string, storedPath string) (bool, error) {
	p := strings.TrimSpace(storedPath)
	if p == "" {
		return false, nil
	}
	abs := resolveUploadsPathAbs(uploadsDir, p)
	if abs == "" {
		return false, fmt.Errorf("无效的存储路径: %s", p)
	}
	if err := os.Remove(abs); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("删除草稿文件 %s 失败: %w", abs, err)
	}
	return true, nil
}

func resolveUploadsPathAbs(uploadsDir, storedPath string) string {
	uploadsDir = strings.TrimSpace(uploadsDir)
	storedPath = strings.TrimSpace(storedPath)
	if uploadsDir == "" || storedPath == "" {
		return ""
	}

	root, err := filepath.Abs(uploadsDir)
	if err != nil {
		return ""
	}
	root = filepath.Clean(root)

	var candidate string
	if filepath.IsAbs(storedPath) {
		candidate = filepath.Clean(storedPath)
	} else {
		// 数据库存储通常以 uploads/ 开头，落盘时相对于上传目录解析。
		p := strings.ReplaceAll(storedPath, "\\", "/")
		p = strings.TrimPrefix(p, "/")
		p = strings.TrimPrefix(p, "uploads/")
		candidate = filepath.Join(root, filepath.FromSlash(p))
		candidate = filepath.Clean(candidate)
	}

	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ""
	}
	return candidate
}
