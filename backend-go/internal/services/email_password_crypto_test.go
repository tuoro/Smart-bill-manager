package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEmailPasswordKeyDoesNotGenerateWhenDisallowed(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "missing.key")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY_FILE", keyPath)

	if _, err := loadEmailPasswordKey(false); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("禁止生成时缺失密钥必须返回错误，实际为 %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("禁止生成时不得创建密钥文件，文件状态错误为 %v", err)
	}
}

func TestLoadEmailPasswordKeyGeneratesWhenAllowed(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "generated.key")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY_FILE", keyPath)

	key, err := loadEmailPasswordKey(true)
	if err != nil {
		t.Fatalf("允许生成时加载密钥失败: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("生成密钥长度错误: %d", len(key))
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("允许生成时未写入密钥文件: %v", err)
	}
}

func TestLoadEmailPasswordKeyRejectsUnreadablePath(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "key-as-directory")
	if err := os.Mkdir(keyPath, 0o700); err != nil {
		t.Fatalf("创建不可读密钥路径失败: %v", err)
	}
	t.Setenv("SBM_EMAIL_PASSWORD_KEY", "")
	t.Setenv("SBM_EMAIL_PASSWORD_KEY_FILE", keyPath)

	if _, err := loadEmailPasswordKey(false); err == nil || !strings.Contains(err.Error(), "read email password key file") {
		t.Fatalf("不可读密钥路径必须返回读取错误，实际为 %v", err)
	}
}

func TestDecryptEmailPasswordWithKeyRejectsWrongKeyAndCorruptCiphertext(t *testing.T) {
	key, err := parseEmailPasswordKey(strings.Repeat("1", 64))
	if err != nil {
		t.Fatalf("解析测试密钥失败: %v", err)
	}
	wrongKey, err := parseEmailPasswordKey(strings.Repeat("2", 64))
	if err != nil {
		t.Fatalf("解析错误测试密钥失败: %v", err)
	}
	encrypted, err := encryptEmailPasswordWithKey("stored-password", key)
	if err != nil {
		t.Fatalf("准备测试密文失败: %v", err)
	}

	if _, err := decryptEmailPasswordWithKey(encrypted, wrongKey); err == nil {
		t.Fatal("错误密钥不得解密历史密文")
	}
	if _, err := decryptEmailPasswordWithKey(emailPasswordEncPrefix+"not-base64!", key); err == nil {
		t.Fatal("损坏密文不得通过解密验证")
	}
}
