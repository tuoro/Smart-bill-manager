package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func testExportManifest() ExportManifest {
	return ExportManifest{Scope: ExportScope{Kind: "trip", ID: "synthetic-trip"}, Version: 1,
		References: []ExportReference{{Kind: "original", RelationID: "a", FactID: "fact-a", DocumentID: "document-b"}, {Kind: "auxiliary", RelationID: "b", FactID: "fact-a", DocumentID: "document-a"}},
		Files:      []ExportFile{{DocumentID: "document-b", OriginalName: "../../CON\r\n.png", MIME: "image/png", SizeBytes: 3, SHA256: strings.Repeat("a", 64)}, {DocumentID: "document-a", OriginalName: "../../CON\r\n.png", MIME: "image/png", SizeBytes: 3, SHA256: strings.Repeat("b", 64)}}}
}

func TestExportManifestCanonicalIdentityAndSafePaths(t *testing.T) {
	input := testExportManifest()
	first, err := CanonicalExportManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceBytes != 6 || first.Files[0].DocumentID != "document-a" || first.Files[0].Path == first.Files[1].Path || strings.Contains(first.Files[0].Path, "..") || strings.ContainsAny(first.Files[0].Path, "\r\n\\") {
		t.Fatal("unsafe or nondeterministic filenames")
	}
	if input.Files[0].Path != "" || input.Files[0].DocumentID != "document-b" || input.ManifestHash != "" {
		t.Fatal("input mutated")
	}
	slices.Reverse(input.Files)
	slices.Reverse(input.References)
	second, err := CanonicalExportManifest(input)
	if err != nil || second.ManifestHash != first.ManifestHash {
		t.Fatal("ordering changed identity")
	}
	input.References[0].RelationID = "changed-relationship"
	third, err := CanonicalExportManifest(input)
	if err != nil || third.ManifestHash == first.ManifestHash {
		t.Fatal("relationship identity omitted from hash")
	}
	if _, err := CanonicalExportManifest(first); err != nil {
		t.Fatal("canonical manifest rejected")
	}
}

func TestExportManifestLimitsAndPermissions(t *testing.T) {
	for _, role := range []Role{RoleOwner, RoleFinance, RoleReviewer, RoleViewer} {
		err := RequireMaterialExport(TenantContext{TenantID: "tenant", UserID: "user", Role: role}, ExportScope{Kind: "reimbursement", ID: "snapshot"})
		if (role == RoleOwner || role == RoleFinance) != (err == nil) {
			t.Fatalf("role %s: %v", role, err)
		}
	}
	if !errors.Is(RequireMaterialExport(TenantContext{}, ExportScope{Kind: "trip", ID: "id"}), ErrUnauthenticated) {
		t.Fatal("anonymous export")
	}
	for _, id := range []string{"", "../trip", "trip?x=1", strings.Repeat("a", 129)} {
		if (ExportScope{Kind: "trip", ID: id}).Valid() {
			t.Fatal("invalid scope accepted")
		}
	}
	if (ExportScope{Kind: "all", ID: "trip"}).Valid() {
		t.Fatal("unbounded scope accepted")
	}
	input := testExportManifest()
	input.References = nil
	if _, err := CanonicalExportManifest(input); !errors.Is(err, ErrConflict) {
		t.Fatal("empty package accepted")
	}
	input = testExportManifest()
	input.Files = append(input.Files, input.Files[0])
	if _, err := CanonicalExportManifest(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("duplicate physical file accepted")
	}
	input = testExportManifest()
	input.References[0].DocumentID = "missing"
	if _, err := CanonicalExportManifest(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing reference accepted")
	}
	input = testExportManifest()
	input.Files[0].SizeBytes = MaxExportSourceBytes
	if _, err := CanonicalExportManifest(input); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatal("source byte limit absent")
	}
	input = testExportManifest()
	input.References = make([]ExportReference, MaxExportReferences+1)
	if _, err := CanonicalExportManifest(input); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatal("reference limit absent")
	}
	input = testExportManifest()
	input.Files = make([]ExportFile, MaxExportFiles+1)
	if _, err := CanonicalExportManifest(input); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatal("file limit absent")
	}
	input = testExportManifest()
	input.Files[0].MIME = "text/plain"
	if _, err := CanonicalExportManifest(input); !errors.Is(err, ErrConflict) {
		t.Fatal("unsupported file accepted")
	}
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp", "application/pdf"} {
		input = testExportManifest()
		input.Files[0].MIME = mime
		input.Files[0].OriginalName = "..."
		if _, err := CanonicalExportManifest(input); err != nil {
			t.Fatal(fmt.Sprintf("supported MIME %s failed", mime))
		}
	}
}
