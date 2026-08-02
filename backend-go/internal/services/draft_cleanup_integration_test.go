//go:build cgo

package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

func TestCleanupDraftsDeletesRowsAndFilesAfterCommit(t *testing.T) {
	db := openServiceTestDB(t)
	uploadsDir := t.TempDir()
	createdAt := time.Now().Add(-2 * time.Hour)

	paymentPath := createCleanupTestFile(t, uploadsDir, "owner/payment.png")
	invoicePath := createCleanupTestFile(t, uploadsDir, "owner/invoice.pdf")
	attachmentPath := createCleanupTestFile(t, uploadsDir, "owner/attachment.pdf")
	paymentStoredPath := "uploads/owner/payment.png"

	payment := models.Payment{
		ID: "old-payment", OwnerUserID: "owner-1", IsDraft: true,
		TransactionTime: createdAt.Format(time.RFC3339), TransactionTimeTs: createdAt.UnixMilli(),
		ScreenshotPath: &paymentStoredPath, TripAssignSrc: "auto", TripAssignState: "no_match",
		DedupStatus: "ok", CreatedAt: createdAt,
	}
	invoice := models.Invoice{
		ID: "old-invoice", OwnerUserID: "owner-1", IsDraft: true,
		Filename: "invoice.pdf", OriginalName: "invoice.pdf", FilePath: "uploads/owner/invoice.pdf",
		ParseStatus: "pending", Source: "upload", DedupStatus: "ok", CreatedAt: createdAt,
	}
	attachment := models.InvoiceAttachment{
		ID: "old-attachment", OwnerUserID: "owner-1", InvoiceID: invoice.ID,
		Kind: "attachment", Filename: "attachment.pdf", OriginalName: "attachment.pdf",
		FilePath: "uploads/owner/attachment.pdf", Source: "upload", CreatedAt: createdAt,
	}
	paymentBlob := models.PaymentOCRBlob{PaymentID: payment.ID, OwnerUserID: "owner-1"}
	invoiceBlob := models.InvoiceOCRBlob{InvoiceID: invoice.ID, OwnerUserID: "owner-1"}

	for _, row := range []any{&payment, &invoice, &attachment, &paymentBlob, &invoiceBlob} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备草稿清理数据失败: %v", err)
		}
	}

	payments, invoices, files, err := cleanupDraftsOnce(context.Background(), db, uploadsDir, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("清理过期草稿失败: %v", err)
	}
	if payments != 1 || invoices != 1 || files != 3 {
		t.Fatalf("清理计数异常: payments=%d invoices=%d files=%d", payments, invoices, files)
	}
	assertCleanupRowCount(t, db, &models.Payment{}, 0)
	assertCleanupRowCount(t, db, &models.Invoice{}, 0)
	assertCleanupRowCount(t, db, &models.InvoiceAttachment{}, 0)
	assertCleanupRowCount(t, db, &models.PaymentOCRBlob{}, 0)
	assertCleanupRowCount(t, db, &models.InvoiceOCRBlob{}, 0)
	for _, path := range []string{paymentPath, invoicePath, attachmentPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("数据库提交后草稿文件应被删除: %s err=%v", path, err)
		}
	}
}

func TestCleanupDraftsKeepsFileWhenTransactionFails(t *testing.T) {
	db := openServiceTestDB(t)
	uploadsDir := t.TempDir()
	filePath := createCleanupTestFile(t, uploadsDir, "owner/payment.png")
	storedPath := "uploads/owner/payment.png"
	createdAt := time.Now().Add(-2 * time.Hour)
	payment := models.Payment{
		ID: "rollback-payment", OwnerUserID: "owner-1", IsDraft: true,
		TransactionTime: createdAt.Format(time.RFC3339), TransactionTimeTs: createdAt.UnixMilli(),
		ScreenshotPath: &storedPath, TripAssignSrc: "auto", TripAssignState: "no_match",
		DedupStatus: "ok", CreatedAt: createdAt,
	}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatalf("准备回滚测试支付记录失败: %v", err)
	}
	if err := db.Migrator().DropTable(&models.PaymentOCRBlob{}); err != nil {
		t.Fatalf("构造清理事务失败条件失败: %v", err)
	}

	payments, invoices, files, err := cleanupDraftsOnce(context.Background(), db, uploadsDir, time.Now().Add(-time.Hour))
	if err == nil {
		t.Fatal("缺少 Blob 表时清理事务应失败")
	}
	if payments != 0 || invoices != 0 || files != 0 {
		t.Fatalf("事务失败不应报告删除: payments=%d invoices=%d files=%d", payments, invoices, files)
	}
	assertCleanupRowCount(t, db, &models.Payment{}, 1)
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("事务失败时文件必须保留: %v", err)
	}
}

func createCleanupTestFile(t *testing.T, root, relative string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("创建测试文件目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	return path
}

func assertCleanupRowCount(t *testing.T, db *gorm.DB, model any, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(model).Count(&count).Error; err != nil {
		t.Fatalf("统计清理数据失败: %v", err)
	}
	if count != want {
		t.Fatalf("清理后的记录数应为 %d，实际为 %d", want, count)
	}
}
