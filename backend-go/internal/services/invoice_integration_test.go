//go:build cgo

package services

import (
	"testing"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

func TestInvoiceServiceUsesInjectedDatabase(t *testing.T) {
	primaryDB := openServiceTestDB(t)
	secondaryDB := openServiceTestDB(t)

	service := NewInvoiceService(primaryDB, t.TempDir())
	fileHash := "invoice-hash"
	invoice, err := service.CreateDraftFromUpload("owner-1", CreateInvoiceInput{
		Filename:     "invoice.pdf",
		OriginalName: "invoice.pdf",
		FilePath:     "uploads/invoice.pdf",
		FileSize:     128,
		FileSHA256:   &fileHash,
		Source:       "upload",
	})
	if err != nil {
		t.Fatalf("创建发票草稿失败: %v", err)
	}
	if invoice.OwnerUserID != "owner-1" || !invoice.IsDraft {
		t.Fatalf("发票草稿内容异常: %#v", invoice)
	}

	assertInvoiceCount(t, primaryDB, 1)
	assertInvoiceCount(t, secondaryDB, 0)

	found, err := service.FindByFileSHA256ForOwner("owner-1", fileHash, "")
	if err != nil {
		t.Fatalf("按文件哈希查询发票失败: %v", err)
	}
	if found == nil || found.ID != invoice.ID {
		t.Fatalf("去重查询未使用注入数据库: %#v", found)
	}
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
