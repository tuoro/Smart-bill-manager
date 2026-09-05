package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveryRequiresExplicitScopeAndPrivateInput(t *testing.T) {
	for _, args := range [][]string{nil, {"--password=forbidden"}, {"--confirm-all-workspaces", "unexpected"}, {"--confirm-all-workspaces=false"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatal("unconfirmed or credential argv accepted")
		}
	}
	if _, err := parseOptions([]string{"--confirm-all-workspaces", "--input-file=/synthetic/input"}); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecoveryInput("", os.Stdin, io.Discard); err == nil {
		t.Fatal("non-terminal password input accepted")
	}
	path := filepath.Join(t.TempDir(), "input")
	valid := `{"email":"owner@example.invalid","new_password":"synthetic-password","reason":"合成恢复"}`
	for _, payload := range []string{`{}`, `[]`, `{"email":null,"new_password":"x","reason":"x"}`, `{"email":"x","email":"y","new_password":"x","reason":"x"}`, `{"Email":"x","new_password":"x","reason":"x"}`, valid + ` {}`, strings.Repeat("x", 8193)} {
		if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRecoveryInput(path, os.Stdin, io.Discard); err == nil {
			t.Fatal("invalid recovery input accepted")
		}
	}
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := readRecoveryInput(path, os.Stdin, io.Discard)
	if err != nil || value.Email != "owner@example.invalid" || value.Reason != "合成恢复" {
		t.Fatal("private recovery input rejected")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecoveryInput(path, os.Stdin, io.Discard); err == nil {
		t.Fatal("public recovery input accepted")
	}
}
