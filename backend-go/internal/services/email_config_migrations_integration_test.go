//go:build cgo

package services

import (
	"strings"
	"testing"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

const testEmailPasswordKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEnsureEmailConfigPasswordsEncryptedIsIdempotent(t *testing.T) {
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", testEmailPasswordKey)
	db := openServiceTestDB(t)
	existingEncrypted, err := encryptEmailPassword("stored-password")
	if err != nil {
		t.Fatalf("准备已有密文失败: %v", err)
	}
	configs := []models.EmailConfig{
		newEmailConfigMigrationTestRow("config-plain", "legacy-password"),
		newEmailConfigMigrationTestRow("config-encrypted", existingEncrypted),
	}
	if err := db.Create(&configs).Error; err != nil {
		t.Fatalf("写入邮箱配置失败: %v", err)
	}

	if err := EnsureEmailConfigPasswordsEncrypted(db); err != nil {
		t.Fatalf("迁移明文邮箱密码失败: %v", err)
	}
	plain := readEmailConfigMigrationPassword(t, db, "config-plain")
	if !strings.HasPrefix(plain, emailPasswordEncPrefix) || plain == "legacy-password" {
		t.Fatalf("明文邮箱密码未加密: %q", plain)
	}
	decrypted, err := decryptEmailPassword(plain)
	if err != nil {
		t.Fatalf("解密迁移后的邮箱密码失败: %v", err)
	}
	if decrypted != "legacy-password" {
		t.Fatalf("迁移后的邮箱密码内容错误: %q", decrypted)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-encrypted"); got != existingEncrypted {
		t.Fatalf("已有密文不应被改写: %q", got)
	}

	if err := EnsureEmailConfigPasswordsEncrypted(db); err != nil {
		t.Fatalf("重复执行邮箱密码迁移失败: %v", err)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-plain"); got != plain {
		t.Fatal("重复执行不应重新加密已有密文")
	}
}

func TestEnsureEmailConfigPasswordsEncryptedRollsBackAllUpdates(t *testing.T) {
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", testEmailPasswordKey)
	db := openServiceTestDB(t)
	configs := []models.EmailConfig{
		newEmailConfigMigrationTestRow("config-a", "password-a"),
		newEmailConfigMigrationTestRow("config-b", "password-b"),
	}
	if err := db.Create(&configs).Error; err != nil {
		t.Fatalf("写入邮箱配置失败: %v", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER fail_config_b_password_update
		BEFORE UPDATE OF password ON email_configs
		WHEN OLD.id = 'config-b'
		BEGIN
			SELECT RAISE(ABORT, 'forced password update failure');
		END
	`).Error; err != nil {
		t.Fatalf("创建密码更新失败触发器失败: %v", err)
	}

	err := EnsureEmailConfigPasswordsEncrypted(db)
	if err == nil || !strings.Contains(err.Error(), "更新加密邮箱密码失败") {
		t.Fatalf("密码更新失败应向上返回，实际错误为 %v", err)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-a"); got != "password-a" {
		t.Fatalf("首条密码更新未回滚: %q", got)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-b"); got != "password-b" {
		t.Fatalf("失败记录的密码发生变化: %q", got)
	}
}

func newEmailConfigMigrationTestRow(id, password string) models.EmailConfig {
	return models.EmailConfig{
		ID:          id,
		OwnerUserID: "owner-1",
		Email:       id + "@example.com",
		IMAPHost:    "imap.example.com",
		IMAPPort:    993,
		Password:    password,
		IsActive:    1,
	}
}

func readEmailConfigMigrationPassword(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var config models.EmailConfig
	if err := db.Select("id", "password").First(&config, "id = ?", id).Error; err != nil {
		t.Fatalf("读取邮箱配置 %s 失败: %v", id, err)
	}
	return config.Password
}
