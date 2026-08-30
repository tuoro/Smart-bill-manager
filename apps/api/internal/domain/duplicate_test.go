package domain

import (
	"errors"
	"testing"
)

func TestPageVisualFingerprintAndNearBoundary(t *testing.T) {
	fingerprint := NewPageVisualFingerprint(0x0123456789abcdef, 0xfedcba9876543210)
	if fingerprint.Version != PageVisualFingerprintVersion ||
		fingerprint.DHash64 != "0123456789abcdef" ||
		fingerprint.AHash64 != "fedcba9876543210" ||
		fingerprint.DHashBands != [4]int{0x0123, 0x4567, 0x89ab, 0xcdef} {
		t.Fatalf("fingerprint = %#v", fingerprint)
	}
	if err := fingerprint.Validate(); err != nil {
		t.Fatal(err)
	}
	invalidBands := fingerprint
	invalidBands.DHashBands[2]++
	if err := invalidBands.Validate(); err == nil {
		t.Fatal("inconsistent dHash bands were accepted")
	}

	base := visualPage("page-a", "doc-a", 1, 100, 100, 0, 0)
	threshold := visualPage("page-b", "doc-b", 1, 101, 100, 0b111, 0b111)
	near, dhashDistance, ahashDistance, err := VisualPagesNear(base, threshold)
	if err != nil || !near || dhashDistance != 3 || ahashDistance != 3 {
		t.Fatalf("threshold match = near:%t d:%d a:%d err:%v", near, dhashDistance, ahashDistance, err)
	}
	overHashThreshold := visualPage("page-c", "doc-c", 1, 100, 100, 0b1111, 0)
	if near, _, _, err := VisualPagesNear(base, overHashThreshold); err != nil || near {
		t.Fatalf("hash distance 4 = near:%t err:%v", near, err)
	}
	overAspectThreshold := visualPage("page-d", "doc-d", 1, 102, 100, 0, 0)
	if near, _, _, err := VisualPagesNear(base, overAspectThreshold); err != nil || near {
		t.Fatalf("aspect difference over 1%% = near:%t err:%v", near, err)
	}
}

func TestBuildVisualDuplicateSignalsPrioritizesWholeDocument(t *testing.T) {
	current := VisualDocument{ID: "current", Pages: []VisualPage{
		visualPage("current-1", "current", 1, 1200, 1800, 0, 0),
		visualPage("current-2", "current", 2, 1200, 1800, ^uint64(0), ^uint64(0)),
	}}
	nearDocument := VisualDocument{ID: "near", Pages: []VisualPage{
		visualPage("near-1", "near", 1, 1200, 1800, 1, 1),
		visualPage("near-2", "near", 2, 1200, 1800, ^uint64(0)-1, ^uint64(0)-1),
	}}
	partialDocument := VisualDocument{ID: "partial", Pages: []VisualPage{
		visualPage("partial-1", "partial", 1, 1200, 1800, 2, 2),
		visualPage("partial-2", "partial", 2, 1200, 1800, 0xaaaaaaaaaaaaaaaa, 0xaaaaaaaaaaaaaaaa),
	}}

	signals, err := BuildVisualDuplicateSignals(current, []VisualDocument{partialDocument, nearDocument})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 {
		t.Fatalf("signals = %#v", signals)
	}
	if signals[0].Kind != "near_file" || signals[0].ExistingDocumentID != "near" ||
		signals[0].CurrentDocumentPageID != "" || signals[0].ExistingDocumentPageID != "" {
		t.Fatalf("near-file signal = %#v", signals[0])
	}
	if signals[1].Kind != "cross_page" || signals[1].ExistingDocumentID != "partial" ||
		signals[1].CurrentPageNumber != 1 || signals[1].ExistingPageNumber != 1 {
		t.Fatalf("cross-page signal = %#v", signals[1])
	}
}

func TestBuildVisualDuplicateSignalsFindsRepeatedPageWithinDocument(t *testing.T) {
	current := VisualDocument{ID: "current", Pages: []VisualPage{
		visualPage("current-1", "current", 1, 1000, 1400, 0x1234, 0x4321),
		visualPage("current-2", "current", 2, 1000, 1400, 0x1235, 0x4320),
	}}
	signals, err := BuildVisualDuplicateSignals(current, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].Kind != "cross_page" ||
		signals[0].ExistingDocumentID != current.ID ||
		signals[0].CurrentPageNumber != 1 || signals[0].ExistingPageNumber != 2 {
		t.Fatalf("within-document signals = %#v", signals)
	}
}

func TestCanonicalDuplicatePlanAndValidation(t *testing.T) {
	first, firstHash, err := CanonicalDuplicatePlan([]DuplicateResolution{
		{CandidateID: "candidate-b", Action: DuplicateKeepDistinct},
		{CandidateID: "candidate-a", Action: DuplicateKeepDistinct},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := CanonicalDuplicatePlan([]DuplicateResolution{
		{CandidateID: "candidate-a", Action: DuplicateKeepDistinct},
		{CandidateID: "candidate-b", Action: DuplicateKeepDistinct},
	})
	if err != nil || firstHash != secondHash || first[0].CandidateID != "candidate-a" || second[1].CandidateID != "candidate-b" {
		t.Fatalf("canonical plans = %#v %#v hashes:%s/%s err:%v", first, second, firstHash, secondHash, err)
	}
	empty, emptyHash, err := CanonicalDuplicatePlan(nil)
	if err != nil || len(empty) != 0 || len(emptyHash) != 64 {
		t.Fatalf("empty plan = %#v hash:%q err:%v", empty, emptyHash, err)
	}
	if err := ValidateDuplicatePlan(
		[]string{"candidate-a", "candidate-b"},
		second,
	); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDuplicatePlan(
		[]string{"candidate-a", "candidate-b"},
		[]DuplicateResolution{{CandidateID: "candidate-a", Action: DuplicateKeepDistinct}},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing resolution error = %v", err)
	}
	if _, _, err := CanonicalDuplicatePlan([]DuplicateResolution{
		{CandidateID: "candidate-a", Action: DuplicateKeepDistinct},
		{CandidateID: "candidate-a", Action: DuplicateKeepDistinct},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate resolution error = %v", err)
	}
}

func TestBuildFieldDuplicateSignalsUsesFrozenPaymentCombination(t *testing.T) {
	input := FieldDuplicateInput{
		DocumentType:    DocumentPayment,
		AmountMinor:     12345,
		Currency:        "CNY",
		Merchant:        "  ＡＣＭＥ   Store ",
		TransactionTime: "2026-08-30T10:00:00+08:00",
		OrderNumber:     " ORDER-1 ",
	}
	targets := []FieldDuplicateTarget{
		{
			ID: "outside-window", DocumentType: DocumentPayment, AmountMinor: 12345, Currency: "CNY",
			Merchant: "acme store", TransactionTime: "2026-08-30T10:05:01+08:00",
		},
		{
			ID: "match", DocumentType: DocumentPayment, AmountMinor: 12345, Currency: "CNY",
			Merchant: "acme store", TransactionTime: "2026-08-30T10:05:00+08:00", OrderNumber: "order-1",
		},
		{
			ID: "wrong-merchant", DocumentType: DocumentPayment, AmountMinor: 12345, Currency: "CNY",
			Merchant: "other", TransactionTime: "2026-08-30T10:00:00+08:00",
		},
	}
	signals, err := BuildFieldDuplicateSignals(input, targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].ExistingPaymentID != "match" ||
		len(signals[0].ReasonCodes) != 5 || signals[0].ReasonCodes[4] != "order_number_exact" {
		t.Fatalf("payment signals = %#v", signals)
	}
}

func TestBuildFieldDuplicateSignalsKeepsExactInvoiceNumberAsHardRule(t *testing.T) {
	input := FieldDuplicateInput{
		DocumentType:  DocumentInvoice,
		AmountMinor:   5000,
		Currency:      "CNY",
		InvoiceNumber: " INV-1 ",
		InvoiceDate:   "2026-08-30",
		SellerName:    "销售方",
		BuyerName:     "购买方",
	}
	targets := []FieldDuplicateTarget{
		{
			ID: "exact-number", DocumentType: DocumentInvoice, AmountMinor: 5000, Currency: "CNY",
			InvoiceNumber: "inv-1", InvoiceDate: "2026-08-30", SellerName: "销售方", BuyerName: "购买方",
		},
		{
			ID: "field-match", DocumentType: DocumentInvoice, AmountMinor: 5000, Currency: "CNY",
			InvoiceNumber: "INV-2", InvoiceDate: "2026-08-30", SellerName: " 销售方 ", BuyerName: "购买方",
		},
	}
	signals, err := BuildFieldDuplicateSignals(input, targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].ExistingInvoiceID != "field-match" {
		t.Fatalf("invoice signals = %#v", signals)
	}
}

func TestDuplicateCandidateKeyIsStableAndShapeValidated(t *testing.T) {
	distance := 1
	spec := DuplicateCandidateSpec{
		Kind:               "near_file",
		ExistingDocumentID: "document-b",
		DHashDistance:      &distance,
		AHashDistance:      &distance,
		ReasonCodes:        []string{"same_page_count", "ordered_page_visual_match"},
	}
	first, err := DuplicateCandidateKey("tenant", "claim", spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DuplicateCandidateKey("tenant", "claim", spec)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("keys = %q/%q err:%v", first, second, err)
	}
	invalid := spec
	invalid.ExistingPaymentID = "payment"
	if _, err := DuplicateCandidateKey("tenant", "claim", invalid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid candidate shape error = %v", err)
	}
}

func visualPage(
	id, documentID string,
	pageNumber, width, height int,
	dhash, ahash uint64,
) VisualPage {
	return VisualPage{
		ID:          id,
		DocumentID:  documentID,
		PageNumber:  pageNumber,
		Width:       width,
		Height:      height,
		Fingerprint: NewPageVisualFingerprint(dhash, ahash),
	}
}
