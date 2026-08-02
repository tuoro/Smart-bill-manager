//go:build cgo

package services

import (
	"testing"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

func TestInvoiceServiceUsesInjectedDatabase(t *testing.T) {
	primaryDB := openServiceTestDB(t)
	globalDB := openServiceTestDB(t)
	if database.GetDB() != globalDB {
		t.Fatal("测试前提不成立：全局连接应指向第二个数据库")
	}

	service := NewInvoiceService(primaryDB, t.TempDir())
	invoice, err := service.CreateDraftFromUpload("owner-1", CreateInvoiceInput{
		Filename:     "invoice.pdf",
		OriginalName: "invoice.pdf",
		FilePath:     "uploads/invoice.pdf",
		FileSize:     128,
		Source:       "upload",
	})
	if err != nil {
		t.Fatalf("创建发票草稿失败: %v", err)
	}
	if invoice.OwnerUserID != "owner-1" || !invoice.IsDraft {
		t.Fatalf("发票草稿内容异常: %#v", invoice)
	}

	assertInvoiceCount(t, primaryDB, 1)
	assertInvoiceCount(t, globalDB, 0)
}

func assertInvoiceCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.Invoice{}).Count(&count).Error; err != nil {
		t.Fatalf("统计发票记录失败: %v", err)
	}
	if count != want {
		t.Fatalf("发票记录数应为 %d，实际为 %d", want, count)
	}
}
