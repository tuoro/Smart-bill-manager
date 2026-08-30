package domain

import "encoding/json"

type CandidateEvidence struct {
	Page   int             `json:"page"`
	Quote  string          `json:"quote,omitempty"`
	Region json.RawMessage `json:"region,omitempty"`
}

type FieldCandidate struct {
	Path            string              `json:"path"`
	ValueType       string              `json:"value_type"`
	Presence        string              `json:"presence"`
	Value           json.RawMessage     `json:"value,omitempty"`
	NormalizedValue json.RawMessage     `json:"normalized_value,omitempty"`
	Evidence        []CandidateEvidence `json:"evidence,omitempty"`
	Issues          []string            `json:"issues"`
}

type ClaimEnvelope struct {
	SchemaVersion  string           `json:"schema_version"`
	DocumentType   string           `json:"document_type"`
	Fields         []FieldCandidate `json:"fields"`
	DocumentIssues []string         `json:"document_issues"`
}

// BillVisibleTextEnvelope preserves the model's minimal visible-text JSON.
// Only the root identity has passed bill-visible-text/2; the Claim Mapper owns
// field-level decoding, deterministic normalization, evidence construction and
// local validation issues.
type BillVisibleTextEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	DocumentType  string          `json:"document_type"`
	Payment       json.RawMessage `json:"payment"`
	Invoice       json.RawMessage `json:"invoice"`
	Trip          json.RawMessage `json:"trip"`
}
