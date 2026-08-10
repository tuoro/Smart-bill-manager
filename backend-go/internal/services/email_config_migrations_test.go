package services

import "testing"

func TestEnsureEmailConfigPasswordsEncryptedRejectsNilDatabase(t *testing.T) {
	if err := EnsureEmailConfigPasswordsEncrypted(nil); err == nil {
		t.Fatal("数据库连接为空时必须返回错误")
	}
}
