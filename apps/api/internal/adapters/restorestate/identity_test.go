package restorestate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOrdinaryObjectRootMustExistWithoutRestoreIdentity(t *testing.T) {
	root := t.TempDir()
	if CheckObjects(root, Identity{}) != nil {
		t.Fatal("ordinary existing root rejected")
	}
	if CheckObjects(filepath.Join(root, "missing"), Identity{}) == nil {
		t.Fatal("ordinary missing root accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if CheckObjects(link, Identity{}) == nil {
		t.Fatal("ordinary linked root accepted")
	}
}

func TestRestoreObjectIdentityRequiresExactPair(t *testing.T) {
	root := t.TempDir()
	identity := Identity{Version: 1, RestoreID: "0123456789abcdef0123456789abcdef", DatabaseOID: 123, DatabaseName: "synthetic"}
	if CheckObjects(root, Identity{}) != nil || CheckObjects(root, identity) == nil {
		t.Fatal("absent ordinary and restored identities were confused")
	}
	if err := Write(root, identity); err != nil {
		t.Fatal(err)
	}
	if err := CheckObjects(root, identity); err != nil {
		t.Fatal(err)
	}
	if CheckObjects(root, Identity{}) == nil {
		t.Fatal("orphan identity accepted")
	}
	other := identity
	other.DatabaseOID++
	if CheckObjects(root, other) == nil {
		t.Fatal("different database accepted")
	}
	if Write(root, identity) == nil {
		t.Fatal("existing identity overwritten")
	}
}

func TestRestoreObjectIdentityRejectsCorruptionAndUnsafeFiles(t *testing.T) {
	identity := Identity{Version: 1, RestoreID: "0123456789abcdef0123456789abcdef", DatabaseOID: 123, DatabaseName: "synthetic"}
	for _, mode := range []string{"corrupt", "permission", "symlink", "hardlink", "parent_symlink", "duplicate_field"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := Write(root, identity); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, FileName)
			switch mode {
			case "corrupt":
				if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "permission":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".real", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "parent_symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(root, alias); err != nil {
					t.Fatal(err)
				}
				root = alias
			case "duplicate_field":
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				content = append([]byte(`{"version":1,`), content[1:]...)
				if err := os.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if CheckObjects(root, identity) == nil {
				t.Fatal("unsafe identity accepted")
			}
		})
	}
}
