package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUploadsFilePath(t *testing.T) {
	uploadsDir := t.TempDir()

	t.Run("Resolves uploads-prefixed path", func(t *testing.T) {
		got, err := resolveUploadsFilePath(uploadsDir, "uploads/test.png")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		want := filepath.Join(uploadsDir, "test.png")
		if got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
	})

	t.Run("Blocks path traversal", func(t *testing.T) {
		_, err := resolveUploadsFilePath(uploadsDir, "uploads/../secret.png")
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("Blocks absolute path outside uploadsDir", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "outside.png")
		_, err := resolveUploadsFilePath(uploadsDir, outside)
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("Blocks symlink escaping uploadsDir", func(t *testing.T) {
		outsideDir := t.TempDir()
		outside := filepath.Join(outsideDir, "outside.png")
		if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
			t.Fatalf("创建外部测试文件失败: %v", err)
		}
		link := filepath.Join(uploadsDir, "link.png")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("当前环境无法创建符号链接: %v", err)
		}
		if _, err := resolveUploadsFilePath(uploadsDir, link); err == nil {
			t.Fatal("指向上传目录外部的符号链接应被拒绝")
		}

		dirLink := filepath.Join(uploadsDir, "linked-dir")
		if err := os.Symlink(outsideDir, dirLink); err != nil {
			t.Skipf("当前环境无法创建目录符号链接: %v", err)
		}
		if _, err := resolveUploadsFilePath(uploadsDir, filepath.Join(dirLink, "missing.png")); err == nil {
			t.Fatal("符号链接目录中的不存在目标也应被拒绝")
		}
	})
}
