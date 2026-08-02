//go:build cgo

package services

import (
	"testing"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

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
