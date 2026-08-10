//go:build cgo

package services

import (
	"testing"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

func TestEmailServiceUsesInjectedDatabase(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	primaryDB := openServiceTestDB(t)
	secondaryDB := openServiceTestDB(t)

	service := NewEmailService(primaryDB, t.TempDir(), nil)
	config, err := service.CreateConfig("owner-1", CreateEmailConfigInput{
		Email:    "owner@example.com",
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
		Password: "mail-password",
		IsActive: 1,
	})
	if err != nil {
		t.Fatalf("创建邮箱配置失败: %v", err)
	}
	if config.OwnerUserID != "owner-1" || config.Password == "mail-password" {
		t.Fatalf("邮箱配置内容或密码加密异常: %#v", config)
	}

	assertEmailConfigCount(t, primaryDB, 1)
	assertEmailConfigCount(t, secondaryDB, 0)
}

func assertEmailConfigCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.EmailConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("统计邮箱配置失败: %v", err)
	}
	if count != want {
		t.Fatalf("邮箱配置数应为 %d，实际为 %d", want, count)
	}
}
