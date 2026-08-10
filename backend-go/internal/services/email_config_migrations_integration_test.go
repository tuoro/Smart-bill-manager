//go:build cgo

package services

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

const testEmailPasswordKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEnsureEmailConfigPasswordsEncryptedIsIdempotent(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", testEmailPasswordKey)
	db := openServiceTestDB(t)
	existingEncrypted := encryptEmailPasswordForMigrationTest(t, "stored-password", testEmailPasswordKey)
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

func TestEnsureEmailConfigPasswordsEncryptedRejectsMissingKeyForExistingCiphertext(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	keyPath := filepath.Join(t.TempDir(), "missing.key")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY_FILE", keyPath)
	db := openServiceTestDB(t)
	existingEncrypted := encryptEmailPasswordForMigrationTest(t, "stored-password", testEmailPasswordKey)
	config := newEmailConfigMigrationTestRow("config-encrypted", existingEncrypted)
	if err := db.Create(&config).Error; err != nil {
		t.Fatalf("写入已有密文失败: %v", err)
	}

	err := EnsureEmailConfigPasswordsEncrypted(db)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("历史密文缺少密钥时必须拒绝启动修复，实际错误为 %v", err)
	}
	if _, statErr := os.Stat(keyPath); !os.IsNotExist(statErr) {
		t.Fatalf("历史密文存在时不得生成新密钥，文件状态错误为 %v", statErr)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-encrypted"); got != existingEncrypted {
		t.Fatal("缺少密钥时已有密文不得发生变化")
	}
}

func TestEnsureEmailConfigPasswordsEncryptedRejectsWrongKey(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", strings.Repeat("a", 64))
	db := openServiceTestDB(t)
	existingEncrypted := encryptEmailPasswordForMigrationTest(t, "stored-password", testEmailPasswordKey)
	config := newEmailConfigMigrationTestRow("config-encrypted", existingEncrypted)
	if err := db.Create(&config).Error; err != nil {
		t.Fatalf("写入已有密文失败: %v", err)
	}

	err := EnsureEmailConfigPasswordsEncrypted(db)
	if err == nil || !strings.Contains(err.Error(), "验证已有加密邮箱密码失败") {
		t.Fatalf("错误密钥必须阻止启动修复，实际错误为 %v", err)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-encrypted"); got != existingEncrypted {
		t.Fatal("密钥错误时已有密文不得发生变化")
	}
}

func TestEnsureEmailConfigPasswordsEncryptedRejectsUnreadableKeyFile(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	keyPath := filepath.Join(t.TempDir(), "key-as-directory")
	if err := os.Mkdir(keyPath, 0o700); err != nil {
		t.Fatalf("创建不可读密钥路径失败: %v", err)
	}
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY_FILE", keyPath)
	db := openServiceTestDB(t)
	existingEncrypted := encryptEmailPasswordForMigrationTest(t, "stored-password", testEmailPasswordKey)
	config := newEmailConfigMigrationTestRow("config-encrypted", existingEncrypted)
	if err := db.Create(&config).Error; err != nil {
		t.Fatalf("写入已有密文失败: %v", err)
	}

	err := EnsureEmailConfigPasswordsEncrypted(db)
	if err == nil || !strings.Contains(err.Error(), "read email password key file") {
		t.Fatalf("密钥文件不可读时必须阻止启动修复，实际错误为 %v", err)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-encrypted"); got != existingEncrypted {
		t.Fatal("密钥文件不可读时已有密文不得发生变化")
	}
}

func TestEnsureEmailConfigPasswordsEncryptedRejectsCorruptCiphertextAtomically(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", testEmailPasswordKey)
	db := openServiceTestDB(t)
	configs := []models.EmailConfig{
		newEmailConfigMigrationTestRow("config-plain", "legacy-password"),
		newEmailConfigMigrationTestRow("config-corrupt", emailPasswordEncPrefix+"not-base64!"),
	}
	if err := db.Create(&configs).Error; err != nil {
		t.Fatalf("写入混合邮箱密码失败: %v", err)
	}

	err := EnsureEmailConfigPasswordsEncrypted(db)
	if err == nil || !strings.Contains(err.Error(), "验证已有加密邮箱密码失败") {
		t.Fatalf("损坏密文必须阻止启动修复，实际错误为 %v", err)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-plain"); got != "legacy-password" {
		t.Fatalf("密文校验失败时明文迁移不得写入: %q", got)
	}
	if got := readEmailConfigMigrationPassword(t, db, "config-corrupt"); got != emailPasswordEncPrefix+"not-base64!" {
		t.Fatalf("损坏密文不得发生变化: %q", got)
	}
}

func TestEnsureEmailConfigPasswordsEncryptedGeneratesKeyForFirstPlaintextMigration(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	keyPath := filepath.Join(t.TempDir(), "generated.key")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY_FILE", keyPath)
	db := openServiceTestDB(t)
	config := newEmailConfigMigrationTestRow("config-plain", "legacy-password")
	if err := db.Create(&config).Error; err != nil {
		t.Fatalf("写入明文邮箱密码失败: %v", err)
	}

	if err := EnsureEmailConfigPasswordsEncrypted(db); err != nil {
		t.Fatalf("首次明文迁移应生成密钥并完成加密: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("首次明文迁移未生成密钥文件: %v", err)
	}
	stored := readEmailConfigMigrationPassword(t, db, "config-plain")
	plain, err := decryptEmailPassword(stored)
	if err != nil {
		t.Fatalf("解密首次迁移结果失败: %v", err)
	}
	if plain != "legacy-password" {
		t.Fatalf("首次迁移后的密码内容错误: %q", plain)
	}
}

func TestEnsureEmailConfigPasswordsEncryptedDoesNotGenerateKeyWithoutPasswords(t *testing.T) {
	resetEmailPasswordKeyCache(t)
	keyPath := filepath.Join(t.TempDir(), "unused.key")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY_FILE", keyPath)
	db := openServiceTestDB(t)

	if err := EnsureEmailConfigPasswordsEncrypted(db); err != nil {
		t.Fatalf("没有待处理密码时启动修复失败: %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("没有待处理密码时不得生成密钥，文件状态错误为 %v", err)
	}
}

func TestEnsureEmailConfigPasswordsEncryptedRollsBackAllUpdates(t *testing.T) {
	resetEmailPasswordKeyCache(t)
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

func encryptEmailPasswordForMigrationTest(t *testing.T, plain, rawKey string) string {
	t.Helper()
	key, err := parseEmailPasswordKey(rawKey)
	if err != nil {
		t.Fatalf("解析测试密钥失败: %v", err)
	}
	encrypted, err := encryptEmailPasswordWithKey(plain, key)
	if err != nil {
		t.Fatalf("准备已有密文失败: %v", err)
	}
	return encrypted
}

func resetEmailPasswordKeyCache(t *testing.T) {
	t.Helper()
	reset := func() {
		emailPasswordKeyOnce = sync.Once{}
		emailPasswordKey = nil
		emailPasswordKeyErr = nil
	}
	reset()
	t.Cleanup(reset)
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
