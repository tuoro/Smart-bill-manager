package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPasswordAcceptsOnlyOwnerProtectedRegularFile(t *testing.T) {
	root := t.TempDir()
	passwordFile := filepath.Join(root, "owner-password")
	secret := []byte("synthetic-owner-password-123\r\n")
	if err := os.WriteFile(passwordFile, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	password, err := readPassword(passwordFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(password, []byte("synthetic-owner-password-123")) {
		t.Fatalf("password line ending was not trimmed exactly: %q", password)
	}
	if strings.Contains(passwordFile, string(password)) {
		t.Fatal("test precondition invalid: password leaked through file name")
	}

	if err := os.Chmod(passwordFile, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readPassword(passwordFile); err == nil || strings.Contains(err.Error(), string(password)) {
		t.Fatalf("unsafe password file error = %v", err)
	}
}

func TestReadPasswordRejectsSymlinkDirectoryOversizeAndNonTerminalInput(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("synthetic-password"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "password-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"symlink": link, "directory": root} {
		if _, err := readPassword(path); err == nil {
			t.Fatalf("%s password source was accepted", name)
		}
	}
	oversized := filepath.Join(root, "oversized")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte("x"), 1026), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPassword(oversized); err == nil {
		t.Fatal("oversized password source was accepted")
	}
	if _, err := readPassword(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing password source was accepted")
	}
	if _, err := readPassword(""); err == nil {
		t.Fatal("non-terminal password input was accepted without -password-file")
	}
}

func TestTrimSingleLineEndingDoesNotNormalizePasswordBody(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{input: "secret", want: "secret"},
		{input: "secret\n", want: "secret"},
		{input: "secret\r\n", want: "secret"},
		{input: "secret\n\n", want: "secret\n"},
		{input: "secret\r", want: "secret\r"},
	}
	for _, item := range cases {
		if got := string(trimSingleLineEnding([]byte(item.input))); got != item.want {
			t.Fatalf("trimSingleLineEnding(%q) = %q, want %q", item.input, got, item.want)
		}
	}
}

func TestReadPasswordErrorsNeverWrapSecretContent(t *testing.T) {
	root := t.TempDir()
	secret := "do-not-log-this-password"
	path := filepath.Join(root, "unsafe")
	if err := os.WriteFile(path, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readPassword(path)
	if err == nil {
		t.Fatal("unsafe password source was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("password leaked in error: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected error classification: %v", err)
	}
}
