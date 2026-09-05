package cryptography

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateFileRejectsAliasesModesAndUnboundedInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value")
	if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadPrivateFile(path, 9); err != nil || string(value) != "synthetic" {
		t.Fatal("valid private file rejected")
	}
	for _, limit := range []int64{0, 8} {
		if _, err := ReadPrivateFile(path, limit); err == nil {
			t.Fatal("size boundary ignored")
		}
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(link, 9); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Link(path, filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path, 9); err == nil {
		t.Fatal("multiple-link file accepted")
	}
	for _, name := range []string{"missing", "empty", "fifo"} {
		file := filepath.Join(root, name)
		if name == "empty" {
			if err := os.WriteFile(file, nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if name == "fifo" {
			if err := unix.Mkfifo(file, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ReadPrivateFile(file, 9); err == nil {
			t.Fatal("non-readable private input accepted")
		}
	}
	if _, err := ReadPrivateFile(root, 9); err == nil {
		t.Fatal("directory accepted")
	}
}
