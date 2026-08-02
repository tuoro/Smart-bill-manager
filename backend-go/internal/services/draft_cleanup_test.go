package services

import (
	"path/filepath"
	"testing"
)

func TestResolveUploadsPathAbsRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "owner", "invoice.pdf")
	outside := filepath.Join(filepath.Dir(root), "outside.pdf")

	if got := resolveUploadsPathAbs(root, "uploads/owner/invoice.pdf"); got != inside {
		t.Fatalf("相对上传路径解析错误: got=%s want=%s", got, inside)
	}
	if got := resolveUploadsPathAbs(root, inside); got != inside {
		t.Fatalf("目录内绝对路径解析错误: got=%s want=%s", got, inside)
	}
	for _, unsafePath := range []string{"../outside.pdf", outside, root} {
		if got := resolveUploadsPathAbs(root, unsafePath); got != "" {
			t.Fatalf("越界路径应被拒绝: path=%s got=%s", unsafePath, got)
		}
	}
}
