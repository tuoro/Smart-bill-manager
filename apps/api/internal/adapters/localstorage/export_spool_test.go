package localstorage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestExportSpoolIsAnonymousAndDoesNotTouchObjects(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeExportSpool(); err != nil {
		t.Fatal(err)
	}
	file, err := store.CreateExportFile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.(*os.File).Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Nlink != 0 {
		t.Fatal("temporary file is named or not private")
	}
	if _, err := file.Write([]byte("synthetic package")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, exportDirectory))
	if err != nil || len(entries) != 0 {
		t.Fatal("anonymous file visible in directory")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(file)
	if err != nil || string(body) != "synthetic package" {
		t.Fatal("unlinked file unreadable")
	}
	if err := store.InitializeExportSpool(); err != nil {
		t.Fatal("startup invalidated live anonymous file")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Read(make([]byte, 1)); err == nil {
		t.Fatal("closed spool remains readable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.CreateExportFile(ctx); err == nil {
		t.Fatal("cancelled creation accepted")
	}
}

func TestExportSpoolStartupOnlyRemovesOwnEmptyCreationResidue(t *testing.T) {
	for _, kind := range []string{"empty", "nonempty", "unknown", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			store, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.InitializeExportSpool(); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(root, exportDirectory, exportPrefix+"synthetic")
			target := filepath.Join(root, "objects", "synthetic-original")
			if err := os.WriteFile(target, []byte("synthetic protected object"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "empty":
				err = os.WriteFile(name, nil, 0600)
			case "nonempty":
				err = os.WriteFile(name, []byte("partial"), 0600)
			case "unknown":
				name = filepath.Join(root, exportDirectory, "user-file")
				err = os.WriteFile(name, nil, 0600)
			case "symlink":
				err = os.Symlink(target, name)
			case "hardlink":
				err = os.Link(target, name)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = store.InitializeExportSpool()
			if kind == "empty" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = os.Lstat(name); !os.IsNotExist(err) {
					t.Fatal("empty residue retained")
				}
			} else {
				if err == nil {
					t.Fatal("unexpected file accepted")
				}
				if _, err = os.Lstat(name); err != nil {
					t.Fatal("unknown file removed")
				}
			}
			body, err := os.ReadFile(target)
			if err != nil || string(body) != "synthetic protected object" {
				t.Fatal("business object touched")
			}
		})
	}
}
