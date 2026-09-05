package reviews

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestManualRevisionUsesExplicitAnnotationsWithoutInventingQuotes(t *testing.T) {
	current := ports.ReviewSnapshot{PageCount: 2, DocumentPageIDs: map[int]string{1: "page-one", 2: "page-two"}}
	input := RevisionInput{Fields: []RevisionFieldInput{{Path: "merchant", ValueType: "string", Presence: "present", Value: json.RawMessage(`"人工录入商户"`),
		ManualEvidence: []domain.ManualEvidenceInput{{Page: 2, Quote: "第2页的实际摘录"}}}}}
	fields, selected, err := revisionCandidates(current, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || len(fields[0].Evidence) != 1 || fields[0].Evidence[0].Quote != "第2页的实际摘录" {
		t.Fatal("annotation changed or missing")
	}
	if selected["merchant"][0].DocumentPageID != "page-two" || selected["merchant"][0].ID != "" {
		t.Fatal("annotation invented an existing evidence identity")
	}
	current.OriginAiRunID = "actual-ai-run"
	if _, _, err := revisionCandidates(current, input); err == nil {
		t.Fatal("AI review accepted manual-only input")
	}
}

func TestManualRevisionCountsExistingAndNewEvidenceTogether(t *testing.T) {
	current := ports.ReviewSnapshot{PageCount: 1, DocumentPageIDs: map[int]string{1: "page"}}
	old := ports.ReviewField{Path: "merchant", ValueType: "string", Presence: "present", Value: json.RawMessage(`"商户"`)}
	for index := 0; index < 8; index++ {
		old.Evidence = append(old.Evidence, ports.ReviewEvidence{ID: fmt.Sprintf("e%d", index), Page: 1, DocumentPageID: "page", Quote: "摘录"})
	}
	current.Fields = []ports.ReviewField{old}
	input := RevisionInput{Fields: []RevisionFieldInput{{Path: old.Path, ValueType: old.ValueType, Presence: old.Presence, Value: old.Value, ManualEvidence: []domain.ManualEvidenceInput{{Page: 1, Quote: "新增"}}}}}
	if _, _, err := revisionCandidates(current, input); err == nil {
		t.Fatal("combined evidence exceeded shared limit")
	}
	input.Fields[0].EvidenceIDs = []string{"e0"}
	if _, _, err := revisionCandidates(current, input); err != nil {
		t.Fatal(err)
	}
}

func TestManualRevisionRejectsMissingOrInvalidEvidence(t *testing.T) {
	current := ports.ReviewSnapshot{PageCount: 2, DocumentPageIDs: map[int]string{1: "page-one"}}
	for name, evidence := range map[string][]domain.ManualEvidenceInput{
		"missing": nil, "unknown page identity": {{Page: 2, Quote: "摘录"}}, "empty quote": {{Page: 1, Quote: " "}}, "out of range": {{Page: 3, Quote: "摘录"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := revisionCandidates(current, RevisionInput{Fields: []RevisionFieldInput{{Path: "merchant", ValueType: "string", Presence: "present", Value: json.RawMessage(`"商户"`), ManualEvidence: evidence}}})
			if err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	_, _, err := revisionCandidates(current, RevisionInput{Fields: []RevisionFieldInput{{Path: "merchant", ValueType: "string", Presence: "absent", ManualEvidence: []domain.ManualEvidenceInput{{Page: 1, Quote: "摘录"}}}}})
	if err == nil {
		t.Fatal("absent field retained an annotation")
	}
}
