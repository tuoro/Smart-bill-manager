package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestInvoiceMaterialRequestsBindActionVersionTargetAndReason(t *testing.T) {
	base := InvoiceMaterialRequest{InvoiceID: "synthetic-invoice", Action: "add", DocumentID: "synthetic-document", ExpectedVersion: 1, Reason: "  合成材料  ", IdempotencyKey: "synthetic-request"}
	normalized, hash, err := CanonicalInvoiceMaterialRequest(base)
	if err != nil || normalized.Reason != "合成材料" || !ValidSHA256Hex(hash) || base.Reason != "  合成材料  " {
		t.Fatal("canonical material request mutated or invalid")
	}
	_, same, err := CanonicalInvoiceMaterialRequest(normalized)
	if err != nil || same != hash {
		t.Fatal("canonical hash not stable")
	}
	for _, change := range []func(*InvoiceMaterialRequest){
		func(v *InvoiceMaterialRequest) { v.Action = "remove"; v.LinkID = v.DocumentID; v.DocumentID = "" },
		func(v *InvoiceMaterialRequest) { v.ExpectedVersion++ }, func(v *InvoiceMaterialRequest) { v.DocumentID = "different" },
		func(v *InvoiceMaterialRequest) { v.Reason = "不同合成理由" },
	} {
		value := base
		change(&value)
		_, changed, err := CanonicalInvoiceMaterialRequest(value)
		if err != nil || changed == hash {
			t.Fatal("material request change not bound")
		}
	}
	for _, change := range []func(*InvoiceMaterialRequest){
		func(v *InvoiceMaterialRequest) { v.ExpectedVersion = 0 }, func(v *InvoiceMaterialRequest) { v.Reason = " " },
		func(v *InvoiceMaterialRequest) { v.Reason = strings.Repeat("字", 501) }, func(v *InvoiceMaterialRequest) { v.IdempotencyKey = "short" },
		func(v *InvoiceMaterialRequest) { v.Action = "unknown" }, func(v *InvoiceMaterialRequest) { v.LinkID = "mixed-target" },
		func(v *InvoiceMaterialRequest) { v.UploadSHA256 = strings.Repeat("a", 64) },
	} {
		value := base
		change(&value)
		if _, _, err := CanonicalInvoiceMaterialRequest(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid material request accepted")
		}
	}
	upload := base
	upload.Action = "upload"
	upload.DocumentID = ""
	upload.UploadSHA256 = strings.Repeat("a", 64)
	upload.UploadName = "synthetic.png"
	upload.UploadMIME = "image/png"
	if _, _, err := CanonicalInvoiceMaterialRequest(upload); err != nil {
		t.Fatal(err)
	}
}

func TestInvoiceMaterialPolicyCanonicalSelectionAndHash(t *testing.T) {
	input := ReimbursementPolicyInput{Trip: ReimbursementTripSnapshot{ID: "synthetic-trip", Name: "合成行程", StartDate: "2026-09-01", EndDate: "2026-09-03"}, Items: []ReimbursementPolicyItem{
		{AssignmentID: "assignment", FactType: DocumentInvoice, FactID: "invoice", DisplayName: "合成发票", BusinessDate: "2026-09-02", AmountMinor: 100, Currency: CurrencyCNY},
	}, Materials: []ReimbursementMaterial{{InvoiceID: "invoice", LinkID: "link-b", DocumentID: "doc-b"}, {InvoiceID: "invoice", LinkID: "link-a", DocumentID: "doc-a"}}}
	original := append([]ReimbursementMaterial{}, input.Materials...)
	first, err := EvaluateReimbursementPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.Materials, original) || first.Materials[0].LinkID != "link-a" {
		t.Fatal("policy did not clone/canonicalize materials")
	}
	reordered := input
	reordered.Materials = []ReimbursementMaterial{input.Materials[1], input.Materials[0]}
	second, err := EvaluateReimbursementPolicy(reordered)
	if err != nil || second.SnapshotHash != first.SnapshotHash {
		t.Fatal("order changed material hash")
	}
	reordered.Materials[0].LinkID = "replacement"
	changed, err := EvaluateReimbursementPolicy(reordered)
	if err != nil || changed.SnapshotHash == first.SnapshotHash {
		t.Fatal("ABA material identity not hashed")
	}
	for _, materials := range [][]ReimbursementMaterial{
		{{InvoiceID: "not-selected", LinkID: "link", DocumentID: "doc"}},
		{{InvoiceID: "invoice", LinkID: "", DocumentID: "doc"}},
		{{InvoiceID: "invoice", LinkID: "link", DocumentID: ""}},
		{input.Materials[0], input.Materials[0]},
		{{InvoiceID: "invoice", LinkID: "link-a", DocumentID: "doc"}, {InvoiceID: "invoice", LinkID: "link-b", DocumentID: "doc"}},
	} {
		bad := input
		bad.Materials = materials
		if _, err := EvaluateReimbursementPolicy(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid selected material accepted")
		}
	}
}

func TestInvoiceMaterialRoleBoundaryRequiresFormalSourceAccess(t *testing.T) {
	for _, role := range []Role{RoleOwner, RoleFinance, RoleReviewer, RoleViewer} {
		err := RequireInvoiceMaterials(TenantContext{TenantID: "synthetic-tenant", UserID: "synthetic-user", Role: role})
		if (role == RoleOwner || role == RoleFinance) != (err == nil) {
			t.Fatalf("role boundary: %s", role)
		}
	}
}
