//go:build cgo

package services

import (
	"os"
	"testing"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

func TestPaymentDeleteKeepsScreenshotWhenTransactionFails(t *testing.T) {
	db := openServiceTestDB(t)
	uploadsDir := t.TempDir()
	filePath := createCleanupTestFile(t, uploadsDir, "owner/payment.png")
	storedPath := "uploads/owner/payment.png"
	service := NewPaymentService(db, uploadsDir)

	payment, err := service.CreateDraftFromScreenshotUpload("owner-1", storedPath, nil)
	if err != nil {
		t.Fatalf("准备删除回滚测试支付记录失败: %v", err)
	}
	if err := db.Migrator().DropTable(&models.PaymentOCRBlob{}); err != nil {
		t.Fatalf("构造删除事务失败条件失败: %v", err)
	}

	if err := service.Delete("owner-1", payment.ID); err == nil {
		t.Fatal("缺少 Blob 表时删除事务应失败")
	}
	assertPaymentCount(t, db, 1)
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("删除事务失败时截图必须保留: %v", err)
	}
}

func TestPaymentServiceUsesInjectedDatabase(t *testing.T) {
	primaryDB := openServiceTestDB(t)
	secondaryDB := openServiceTestDB(t)

	service := NewPaymentService(primaryDB, t.TempDir())
	fileHash := "payment-hash"
	payment, err := service.CreateDraftFromScreenshotUpload("owner-1", "uploads/payment.png", &fileHash)
	if err != nil {
		t.Fatalf("创建支付草稿失败: %v", err)
	}
	if payment.OwnerUserID != "owner-1" || !payment.IsDraft {
		t.Fatalf("支付草稿内容异常: %#v", payment)
	}

	assertPaymentCount(t, primaryDB, 1)
	assertPaymentCount(t, secondaryDB, 0)

	found, err := service.FindByFileSHA256ForOwner("owner-1", fileHash, "")
	if err != nil {
		t.Fatalf("按文件哈希查询支付记录失败: %v", err)
	}
	if found == nil || found.ID != payment.ID {
		t.Fatalf("去重查询未使用注入数据库: %#v", found)
	}
}

func assertPaymentCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.Payment{}).Count(&count).Error; err != nil {
		t.Fatalf("统计支付记录失败: %v", err)
	}
	if count != want {
		t.Fatalf("支付记录数应为 %d，实际为 %d", want, count)
	}
}
