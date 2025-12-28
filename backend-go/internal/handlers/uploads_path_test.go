package handlers

import (
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
}
