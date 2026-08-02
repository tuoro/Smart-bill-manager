//go:build cgo

package services

import (
	"testing"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

func TestPaymentServiceUsesInjectedDatabase(t *testing.T) {
	primaryDB := openServiceTestDB(t)
	globalDB := openServiceTestDB(t)
	if database.GetDB() != globalDB {
		t.Fatal("测试前提不成立：全局连接应指向第二个数据库")
	}

	service := NewPaymentService(primaryDB, t.TempDir())
	payment, err := service.CreateDraftFromScreenshotUpload("owner-1", "uploads/payment.png", nil)
	if err != nil {
		t.Fatalf("创建支付草稿失败: %v", err)
	}
	if payment.OwnerUserID != "owner-1" || !payment.IsDraft {
		t.Fatalf("支付草稿内容异常: %#v", payment)
	}

	assertPaymentCount(t, primaryDB, 1)
	assertPaymentCount(t, globalDB, 0)
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
