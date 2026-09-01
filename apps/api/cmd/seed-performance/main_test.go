package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPerformanceSeedOutputIsReservedBeforeDatabaseWrites(t *testing.T) {
	location := filepath.Join(t.TempDir(), "seed.json")
	result, err := reserveSeedOutput(location)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	content, err := os.ReadFile(location)
	if err != nil || !bytes.Contains(content, []byte("seed-in-progress")) {
		t.Fatalf("reserved seed output = %q, %v", content, err)
	}
	if second, err := reserveSeedOutput(location); err == nil {
		_ = second.Close()
		t.Fatal("existing performance output was reserved twice")
	}
}

func TestPerformanceSeedOutputCannotAliasProtectedPaths(t *testing.T) {
	root := t.TempDir()
	inside, err := pathContains(filepath.Join(root, "migrations"), filepath.Join(root, "migrations", "seed.json"))
	if err != nil || !inside {
		t.Fatalf("nested migration output = %t, %v", inside, err)
	}
	outside, err := pathContains(filepath.Join(root, "migrations"), filepath.Join(root, "results", "seed.json"))
	if err != nil || outside {
		t.Fatalf("external output = %t, %v", outside, err)
	}
	realParent := filepath.Join(root, "real-results")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked-results")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	if err := requireRealSeedOutputParent(filepath.Join(linkedParent, "seed.json")); err == nil {
		t.Fatal("symlinked performance output parent was accepted")
	}
}

func TestPerformanceSeedArgumentsRejectDuplicatesAndUnknownFlags(t *testing.T) {
	valid := []string{"-output", "/tmp/output.json"}
	if _, err := parseSeedArguments(valid); err != nil {
		t.Fatal(err)
	}
	duplicate := []string{"-output", "/tmp/one", "-output", "/tmp/two"}
	if _, err := parseSeedArguments(duplicate); err == nil {
		t.Fatal("duplicate flag was accepted")
	}
	unknown := []string{"-extra", "value"}
	if _, err := parseSeedArguments(unknown); err == nil {
		t.Fatal("unknown flag was accepted")
	}
}

func TestPerformanceSeedOutputParentMustBeOwnerOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requireRealSeedOutputParent(filepath.Join(root, "seed.json")); err == nil {
		t.Fatal("broad output parent was accepted")
	}
}

func TestPerformanceSeedTimeoutHasExplicitSafeError(t *testing.T) {
	if got := safeSeedErrorCode(context.DeadlineExceeded); got != "seed_timeout" {
		t.Fatalf("deadline safe code = %q", got)
	}
}
