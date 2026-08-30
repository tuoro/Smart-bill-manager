package cryptography

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMasterKeyFileAcceptedEncodings(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encodings := map[string][]byte{
		"raw":    append(bytes.Clone(key), '\n'),
		"hex":    append([]byte(hex.EncodeToString(key)), '\r', '\n'),
		"base64": []byte(base64.StdEncoding.EncodeToString(key)),
	}
	for name, content := range encodings {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "master.key")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadMasterKeyFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(loaded, key) {
				t.Fatalf("loaded key differs for %s", name)
			}
		})
	}
}

func TestLoadMasterKeyFileRejectsUnsafeOrInvalidFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadMasterKeyFile(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing master key accepted")
	}
	if _, err := LoadMasterKeyFile(root); err == nil {
		t.Fatal("directory accepted as master key")
	}
	unsafe := filepath.Join(root, "unsafe.key")
	if err := os.WriteFile(unsafe, bytes.Repeat([]byte("a"), 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMasterKeyFile(unsafe); err == nil {
		t.Fatal("group/world-readable master key accepted")
	}
	link := filepath.Join(root, "master-link")
	if err := os.Symlink(unsafe, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMasterKeyFile(link); err == nil {
		t.Fatal("symlink accepted as master key")
	}
	for name, content := range map[string][]byte{
		"too-large": bytes.Repeat([]byte("a"), 129),
		"too-short": []byte("not-a-key"),
		"bad-hex":   bytes.Repeat([]byte("z"), 64),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name+".key")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadMasterKeyFile(path); err == nil {
				t.Fatalf("invalid master key %s accepted", name)
			}
		})
	}
}

func TestTrimLineEndingOnlyRemovesOneTerminalSequence(t *testing.T) {
	if got := string(trimLineEnding([]byte("value\r\n"))); got != "value" {
		t.Fatalf("CRLF trim = %q", got)
	}
	if got := string(trimLineEnding([]byte("value\n\n"))); got != "value\n" {
		t.Fatalf("multiple newline trim = %q", got)
	}
	if got := trimLineEnding(nil); got != nil {
		t.Fatalf("nil trim = %#v", got)
	}
}
