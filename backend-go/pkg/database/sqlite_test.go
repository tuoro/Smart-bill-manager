//go:build cgo

package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesDatabaseAndEnablesForeignKeys(t *testing.T) {
	dataDir := t.TempDir()
	db, err := Open(dataDir)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取数据库句柄失败: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("关闭数据库失败: %v", err)
		}
	})

	if DB != db {
		t.Fatal("全局兼容连接未指向新打开的数据库")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bills.db")); err != nil {
		t.Fatalf("数据库文件未创建: %v", err)
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("读取外键配置失败: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("外键约束应启用，实际为 %d", foreignKeys)
	}
}

func TestOpenReturnsDirectoryCreationError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0600); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	if _, err := Open(filePath); err == nil {
		t.Fatal("数据目录指向普通文件时应返回错误")
	}
}
