package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

const (
	MaxExportReferences        = 5000
	MaxExportFiles             = 1000
	MaxExportSourceBytes int64 = 512 * 1024 * 1024
	MaxExportZIPBytes    int64 = 520 * 1024 * 1024
)

type ExportScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (s ExportScope) Valid() bool {
	return (s.Kind == "trip" || s.Kind == "reimbursement") && ValidExportID(s.ID)
}

func RequireMaterialExport(tenant TenantContext, scope ExportScope) error {
	for _, capability := range []Capability{CapabilityFactsRead, CapabilityReviewSourceRead} {
		if err := tenant.Require(capability); err != nil {
			return err
		}
	}
	if !scope.Valid() {
		return ErrInvalidInput
	}
	if scope.Kind == "reimbursement" {
		return tenant.Require(CapabilityReimbursementsRead)
	}
	return nil
}

type ExportReference struct {
	Kind             string  `json:"kind"`
	RelationID       string  `json:"relation_id"`
	FactType         string  `json:"fact_type"`
	FactID           string  `json:"fact_id"`
	FactVersion      *int    `json:"fact_version"`
	ReviewDecisionID *string `json:"review_decision_id"`
	DisplayName      string  `json:"display_name"`
	BusinessDate     string  `json:"business_date"`
	AmountMinor      *int64  `json:"amount_minor"`
	Currency         string  `json:"currency"`
	DocumentID       string  `json:"document_id"`
}

type ExportFile struct {
	DocumentID   string `json:"document_id"`
	OriginalName string `json:"original_name"`
	Path         string `json:"path"`
	MIME         string `json:"mime"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

type ExportManifest struct {
	SchemaVersion     string                    `json:"schema_version"`
	Scope             ExportScope               `json:"scope"`
	Name              string                    `json:"name"`
	Version           int                       `json:"version"`
	Trip              ReimbursementTripSnapshot `json:"trip"`
	SnapshotHash      string                    `json:"snapshot_hash"`
	MaterialsCaptured bool                      `json:"materials_captured"`
	Warnings          []string                  `json:"warnings"`
	References        []ExportReference         `json:"references"`
	Files             []ExportFile              `json:"files"`
	SourceBytes       int64                     `json:"source_bytes"`
	ManifestHash      string                    `json:"manifest_hash"`
}

func ExportLimit() error {
	return NewRuleError("export_limit", "材料包超过 5,000 个引用、1,000 份文件或 512 MiB 原件上限，不能截断导出", ErrPayloadTooLarge)
}

func ExportObjectUnavailable(documentID string) error {
	return NewRuleError("export_object_unavailable", "材料无法完整读取或校验，请检查单据 "+documentID, ErrConflict)
}

func CanonicalExportManifest(input ExportManifest) (ExportManifest, error) {
	if !input.Scope.Valid() || input.Version < 1 {
		return ExportManifest{}, ErrInvalidInput
	}
	if len(input.References) > MaxExportReferences || len(input.Files) > MaxExportFiles {
		return ExportManifest{}, ExportLimit()
	}
	if len(input.References) == 0 {
		return ExportManifest{}, NewRuleError("export_empty", "所选范围没有可导出的材料", ErrConflict)
	}
	result := input
	result.SchemaVersion = "material-delivery/1"
	result.ManifestHash = ""
	result.SourceBytes = 0
	result.Files = slices.Clone(input.Files)
	result.References = slices.Clone(input.References)
	result.Warnings = slices.Clone(input.Warnings)
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	slices.SortFunc(result.Files, func(a, b ExportFile) int { return strings.Compare(a.DocumentID, b.DocumentID) })
	slices.SortFunc(result.References, func(a, b ExportReference) int {
		if order := strings.Compare(a.Kind, b.Kind); order != 0 {
			return order
		}
		return strings.Compare(a.RelationID, b.RelationID)
	})
	files := make(map[string]bool, len(result.Files))
	for i := range result.Files {
		file := &result.Files[i]
		if !ValidExportID(file.DocumentID) || files[file.DocumentID] || file.SizeBytes < 1 || !ValidSHA256Hex(file.SHA256) {
			return ExportManifest{}, ErrInvalidInput
		}
		extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "application/pdf": ".pdf"}[file.MIME]
		if extension == "" {
			return ExportManifest{}, ExportObjectUnavailable(file.DocumentID)
		}
		if file.SizeBytes > MaxExportSourceBytes-result.SourceBytes {
			return ExportManifest{}, ExportLimit()
		}
		result.SourceBytes += file.SizeBytes
		file.Path = fmt.Sprintf("materials/%04d-%s%s", i+1, exportBaseName(file.OriginalName), extension)
		files[file.DocumentID] = true
	}
	used := make(map[string]bool, len(files))
	relations := make(map[string]bool, len(result.References))
	for _, reference := range result.References {
		key := reference.Kind + ":" + reference.RelationID
		if !files[reference.DocumentID] || relations[key] {
			return ExportManifest{}, ErrInvalidInput
		}
		used[reference.DocumentID], relations[key] = true, true
	}
	if len(used) != len(files) {
		return ExportManifest{}, ErrInvalidInput
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return ExportManifest{}, err
	}
	hash := sha256.Sum256(encoded)
	result.ManifestHash = hex.EncodeToString(hash[:])
	return result, nil
}

func ValidExportID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func exportBaseName(value string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value))
	if len(runes) > 60 {
		runes = runes[:60]
	}
	result := strings.Trim(string(runes), "_-")
	if result == "" {
		return "document"
	}
	return result
}
