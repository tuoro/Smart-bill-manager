package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/bits"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	PageVisualFingerprintVersion  = "page-visual-dedup/1"
	DuplicateDetectionRuleVersion = "duplicate-detection/1"
	MaxDuplicateCandidates        = 50
	MaxVisualHashDistance         = 3
	DuplicateKeepDistinct         = "keep_distinct"
)

type PageVisualFingerprint struct {
	Version    string
	DHash64    string
	AHash64    string
	DHashBands [4]int
}

type VisualPage struct {
	ID          string
	DocumentID  string
	PageNumber  int
	Width       int
	Height      int
	Fingerprint PageVisualFingerprint
}

type VisualDocument struct {
	ID    string
	Pages []VisualPage
}

type VisualDuplicateSignal struct {
	Kind                   string
	ExistingDocumentID     string
	CurrentDocumentPageID  string
	ExistingDocumentPageID string
	CurrentPageNumber      int
	ExistingPageNumber     int
	DHashDistance          int
	AHashDistance          int
	ReasonCodes            []string
}

type DuplicateResolution struct {
	CandidateID string `json:"candidate_id"`
	Action      string `json:"action"`
}

type FieldDuplicateInput struct {
	DocumentType    DocumentType
	AmountMinor     int64
	Currency        string
	Merchant        string
	TransactionTime string
	OrderNumber     string
	InvoiceNumber   string
	InvoiceDate     string
	SellerName      string
	BuyerName       string
}

type FieldDuplicateTarget struct {
	ID              string
	DocumentType    DocumentType
	AmountMinor     int64
	Currency        string
	Merchant        string
	TransactionTime string
	OrderNumber     string
	InvoiceNumber   string
	InvoiceDate     string
	SellerName      string
	BuyerName       string
}

type FieldDuplicateSignal struct {
	ExistingPaymentID string
	ExistingInvoiceID string
	ReasonCodes       []string
}

type DuplicateCandidateSpec struct {
	Kind                   string
	ExistingDocumentID     string
	CurrentDocumentPageID  string
	ExistingDocumentPageID string
	ExistingPaymentID      string
	ExistingInvoiceID      string
	DHashDistance          *int
	AHashDistance          *int
	ReasonCodes            []string
}

func NewPageVisualFingerprint(dhash, ahash uint64) PageVisualFingerprint {
	return PageVisualFingerprint{
		Version: PageVisualFingerprintVersion,
		DHash64: formatVisualHash(dhash),
		AHash64: formatVisualHash(ahash),
		DHashBands: [4]int{
			int((dhash >> 48) & 0xffff),
			int((dhash >> 32) & 0xffff),
			int((dhash >> 16) & 0xffff),
			int(dhash & 0xffff),
		},
	}
}

func (fingerprint PageVisualFingerprint) Validate() error {
	if fingerprint.Version != PageVisualFingerprintVersion {
		return NewRuleError("invalid_visual_fingerprint", "页面视觉指纹版本不受支持", ErrInvalidInput)
	}
	dhash, err := parseVisualHash(fingerprint.DHash64)
	if err != nil {
		return err
	}
	if _, err := parseVisualHash(fingerprint.AHash64); err != nil {
		return err
	}
	expected := NewPageVisualFingerprint(dhash, 0).DHashBands
	if fingerprint.DHashBands != expected {
		return NewRuleError("invalid_visual_fingerprint", "页面视觉指纹检索分段不一致", ErrInvalidInput)
	}
	return nil
}

func VisualPagesNear(left, right VisualPage) (bool, int, int, error) {
	if left.Width < 1 || left.Height < 1 || right.Width < 1 || right.Height < 1 {
		return false, 0, 0, NewRuleError("invalid_visual_page", "页面尺寸不合法", ErrInvalidInput)
	}
	if err := left.Fingerprint.Validate(); err != nil {
		return false, 0, 0, err
	}
	if err := right.Fingerprint.Validate(); err != nil {
		return false, 0, 0, err
	}
	leftRatio := int64(left.Width) * int64(right.Height)
	rightRatio := int64(right.Width) * int64(left.Height)
	difference := leftRatio - rightRatio
	if difference < 0 {
		difference = -difference
	}
	maximum := max(leftRatio, rightRatio)
	if difference*100 > maximum {
		return false, 0, 0, nil
	}
	leftDHash, _ := parseVisualHash(left.Fingerprint.DHash64)
	rightDHash, _ := parseVisualHash(right.Fingerprint.DHash64)
	leftAHash, _ := parseVisualHash(left.Fingerprint.AHash64)
	rightAHash, _ := parseVisualHash(right.Fingerprint.AHash64)
	dhashDistance := bits.OnesCount64(leftDHash ^ rightDHash)
	ahashDistance := bits.OnesCount64(leftAHash ^ rightAHash)
	return dhashDistance <= MaxVisualHashDistance && ahashDistance <= MaxVisualHashDistance,
		dhashDistance,
		ahashDistance,
		nil
}

func BuildVisualDuplicateSignals(current VisualDocument, existing []VisualDocument) ([]VisualDuplicateSignal, error) {
	if err := validateVisualDocument(current); err != nil {
		return nil, err
	}
	result := make([]VisualDuplicateSignal, 0)
	for left := 0; left < len(current.Pages); left++ {
		for right := left + 1; right < len(current.Pages); right++ {
			near, dhashDistance, ahashDistance, err := VisualPagesNear(current.Pages[left], current.Pages[right])
			if err != nil {
				return nil, err
			}
			if !near {
				continue
			}
			result = append(result, VisualDuplicateSignal{
				Kind:                   "cross_page",
				ExistingDocumentID:     current.ID,
				CurrentDocumentPageID:  current.Pages[left].ID,
				ExistingDocumentPageID: current.Pages[right].ID,
				CurrentPageNumber:      current.Pages[left].PageNumber,
				ExistingPageNumber:     current.Pages[right].PageNumber,
				DHashDistance:          dhashDistance,
				AHashDistance:          ahashDistance,
				ReasonCodes:            []string{"visual_page_match", "within_document"},
			})
		}
	}
	seenDocuments := make(map[string]struct{}, len(existing))
	for _, target := range existing {
		if target.ID == current.ID {
			continue
		}
		if _, duplicate := seenDocuments[target.ID]; duplicate {
			return nil, NewRuleError("duplicate_visual_document", "视觉候选文档重复", ErrInvalidInput)
		}
		seenDocuments[target.ID] = struct{}{}
		if err := validateVisualDocument(target); err != nil {
			return nil, err
		}
		wholeDocument, maxDHash, maxAHash, err := visualDocumentsNear(current, target)
		if err != nil {
			return nil, err
		}
		if wholeDocument {
			result = append(result, VisualDuplicateSignal{
				Kind:               "near_file",
				ExistingDocumentID: target.ID,
				DHashDistance:      maxDHash,
				AHashDistance:      maxAHash,
				ReasonCodes:        []string{"same_page_count", "ordered_page_visual_match"},
			})
			continue
		}
		for _, currentPage := range current.Pages {
			for _, targetPage := range target.Pages {
				near, dhashDistance, ahashDistance, err := VisualPagesNear(currentPage, targetPage)
				if err != nil {
					return nil, err
				}
				if !near {
					continue
				}
				result = append(result, VisualDuplicateSignal{
					Kind:                   "cross_page",
					ExistingDocumentID:     target.ID,
					CurrentDocumentPageID:  currentPage.ID,
					ExistingDocumentPageID: targetPage.ID,
					CurrentPageNumber:      currentPage.PageNumber,
					ExistingPageNumber:     targetPage.PageNumber,
					DHashDistance:          dhashDistance,
					AHashDistance:          ahashDistance,
					ReasonCodes:            []string{"visual_page_match", "other_document"},
				})
			}
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return visualSignalLess(result[left], result[right])
	})
	return result, nil
}

func BuildFieldDuplicateSignals(
	input FieldDuplicateInput,
	targets []FieldDuplicateTarget,
) ([]FieldDuplicateSignal, error) {
	if input.DocumentType != DocumentPayment && input.DocumentType != DocumentInvoice {
		return nil, NewRuleError("invalid_duplicate_input", "字段重复检测类型不受支持", ErrInvalidInput)
	}
	if input.AmountMinor <= 0 || input.Currency == "" {
		return []FieldDuplicateSignal{}, nil
	}
	var inputTime time.Time
	var err error
	if input.DocumentType == DocumentPayment {
		inputTime, err = time.Parse(time.RFC3339Nano, input.TransactionTime)
		if err != nil {
			return nil, NewRuleError("invalid_duplicate_input", "支付重复检测时间不合法", ErrInvalidInput)
		}
	}
	seen := make(map[string]struct{}, len(targets))
	result := make([]FieldDuplicateSignal, 0)
	for _, target := range targets {
		if target.ID == "" || target.DocumentType != input.DocumentType {
			return nil, NewRuleError("invalid_duplicate_target", "字段重复检测目标不合法", ErrInvalidInput)
		}
		if _, duplicate := seen[target.ID]; duplicate {
			return nil, NewRuleError("duplicate_duplicate_target", "字段重复检测目标重复", ErrInvalidInput)
		}
		seen[target.ID] = struct{}{}
		if target.AmountMinor != input.AmountMinor || target.Currency != input.Currency {
			continue
		}
		if input.DocumentType == DocumentPayment {
			targetTime, parseErr := time.Parse(time.RFC3339Nano, target.TransactionTime)
			if parseErr != nil {
				return nil, NewRuleError("invalid_duplicate_target", "支付重复检测目标时间不合法", ErrInvalidInput)
			}
			if NormalizeExact(input.Merchant) == "" ||
				NormalizeExact(input.Merchant) != NormalizeExact(target.Merchant) ||
				absoluteDuration(inputTime.Sub(targetTime)) > 5*time.Minute {
				continue
			}
			reasons := []string{"amount_exact", "currency_exact", "merchant_exact", "transaction_time_within_5_minutes"}
			if NormalizeExact(input.OrderNumber) != "" &&
				NormalizeExact(input.OrderNumber) == NormalizeExact(target.OrderNumber) {
				reasons = append(reasons, "order_number_exact")
			}
			result = append(result, FieldDuplicateSignal{
				ExistingPaymentID: target.ID,
				ReasonCodes:       reasons,
			})
			continue
		}
		if NormalizeExact(input.InvoiceNumber) != "" &&
			NormalizeExact(input.InvoiceNumber) == NormalizeExact(target.InvoiceNumber) {
			continue
		}
		if input.InvoiceDate != target.InvoiceDate ||
			NormalizeExact(input.SellerName) == "" ||
			NormalizeExact(input.SellerName) != NormalizeExact(target.SellerName) ||
			NormalizeExact(input.BuyerName) == "" ||
			NormalizeExact(input.BuyerName) != NormalizeExact(target.BuyerName) {
			continue
		}
		result = append(result, FieldDuplicateSignal{
			ExistingInvoiceID: target.ID,
			ReasonCodes: []string{
				"total_exact",
				"currency_exact",
				"invoice_date_exact",
				"seller_exact",
				"buyer_exact",
			},
		})
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ExistingPaymentID+result[left].ExistingInvoiceID <
			result[right].ExistingPaymentID+result[right].ExistingInvoiceID
	})
	return result, nil
}

func BuildDuplicateCandidateSpecs(
	current VisualDocument,
	existing []VisualDocument,
	fieldInput *FieldDuplicateInput,
	fieldTargets []FieldDuplicateTarget,
) ([]DuplicateCandidateSpec, error) {
	visualSignals, err := BuildVisualDuplicateSignals(current, existing)
	if err != nil {
		return nil, err
	}
	result := make([]DuplicateCandidateSpec, 0, len(visualSignals)+len(fieldTargets))
	for _, signal := range visualSignals {
		dhashDistance := signal.DHashDistance
		ahashDistance := signal.AHashDistance
		result = append(result, DuplicateCandidateSpec{
			Kind:                   signal.Kind,
			ExistingDocumentID:     signal.ExistingDocumentID,
			CurrentDocumentPageID:  signal.CurrentDocumentPageID,
			ExistingDocumentPageID: signal.ExistingDocumentPageID,
			DHashDistance:          &dhashDistance,
			AHashDistance:          &ahashDistance,
			ReasonCodes:            append([]string(nil), signal.ReasonCodes...),
		})
	}
	if fieldInput != nil {
		fieldSignals, err := BuildFieldDuplicateSignals(*fieldInput, fieldTargets)
		if err != nil {
			return nil, err
		}
		for _, signal := range fieldSignals {
			result = append(result, DuplicateCandidateSpec{
				Kind:              "field_combination",
				ExistingPaymentID: signal.ExistingPaymentID,
				ExistingInvoiceID: signal.ExistingInvoiceID,
				ReasonCodes:       append([]string(nil), signal.ReasonCodes...),
			})
		}
	}
	return result, nil
}

func DuplicateCandidateKey(tenantID, claimSetID string, spec DuplicateCandidateSpec) (string, error) {
	if tenantID == "" || claimSetID == "" {
		return "", NewRuleError("invalid_duplicate_candidate", "重复候选范围不合法", ErrInvalidInput)
	}
	if err := spec.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal([]string{
		tenantID,
		claimSetID,
		spec.Kind,
		DuplicateDetectionRuleVersion,
		spec.ExistingDocumentID,
		spec.CurrentDocumentPageID,
		spec.ExistingDocumentPageID,
		spec.ExistingPaymentID,
		spec.ExistingInvoiceID,
	})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func (spec DuplicateCandidateSpec) Validate() error {
	visualDistances := spec.DHashDistance != nil && spec.AHashDistance != nil &&
		*spec.DHashDistance >= 0 && *spec.DHashDistance <= MaxVisualHashDistance &&
		*spec.AHashDistance >= 0 && *spec.AHashDistance <= MaxVisualHashDistance
	noFactTarget := spec.ExistingPaymentID == "" && spec.ExistingInvoiceID == ""
	switch spec.Kind {
	case "near_file":
		if spec.ExistingDocumentID == "" || spec.CurrentDocumentPageID != "" ||
			spec.ExistingDocumentPageID != "" || !noFactTarget || !visualDistances {
			return NewRuleError("invalid_duplicate_candidate", "近似文件候选形状不合法", ErrInvalidInput)
		}
	case "cross_page":
		if spec.ExistingDocumentID == "" || spec.CurrentDocumentPageID == "" ||
			spec.ExistingDocumentPageID == "" ||
			spec.CurrentDocumentPageID == spec.ExistingDocumentPageID ||
			!noFactTarget || !visualDistances {
			return NewRuleError("invalid_duplicate_candidate", "跨页重复候选形状不合法", ErrInvalidInput)
		}
	case "field_combination":
		if spec.ExistingDocumentID != "" || spec.CurrentDocumentPageID != "" ||
			spec.ExistingDocumentPageID != "" || spec.DHashDistance != nil ||
			spec.AHashDistance != nil ||
			(spec.ExistingPaymentID == "") == (spec.ExistingInvoiceID == "") {
			return NewRuleError("invalid_duplicate_candidate", "字段组合候选形状不合法", ErrInvalidInput)
		}
	default:
		return NewRuleError("invalid_duplicate_candidate", "重复候选类型不受支持", ErrInvalidInput)
	}
	if len(spec.ReasonCodes) == 0 {
		return NewRuleError("invalid_duplicate_candidate", "重复候选必须包含原因", ErrInvalidInput)
	}
	return nil
}

func CanonicalDuplicatePlan(resolutions []DuplicateResolution) ([]DuplicateResolution, string, error) {
	canonical := make([]DuplicateResolution, len(resolutions))
	copy(canonical, resolutions)
	seen := make(map[string]struct{}, len(canonical))
	for _, resolution := range canonical {
		if resolution.CandidateID == "" || resolution.Action != DuplicateKeepDistinct {
			return nil, "", NewRuleError("invalid_duplicate_resolution", "重复候选决定不合法", ErrInvalidInput)
		}
		if _, duplicate := seen[resolution.CandidateID]; duplicate {
			return nil, "", NewRuleError("duplicate_duplicate_resolution", "同一重复候选只能决定一次", ErrInvalidInput)
		}
		seen[resolution.CandidateID] = struct{}{}
	}
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].CandidateID < canonical[right].CandidateID
	})
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(encoded)
	return canonical, hex.EncodeToString(hash[:]), nil
}

func ValidateDuplicatePlan(candidateIDs []string, resolutions []DuplicateResolution) error {
	if len(candidateIDs) != len(resolutions) {
		return NewRuleError("duplicate_resolution_required", "必须逐项确认全部疑似重复候选", ErrConflict)
	}
	available := make(map[string]struct{}, len(candidateIDs))
	for _, candidateID := range candidateIDs {
		if candidateID == "" {
			return NewRuleError("invalid_duplicate_candidate", "重复候选身份不合法", ErrConflict)
		}
		if _, duplicate := available[candidateID]; duplicate {
			return NewRuleError("invalid_duplicate_candidate", "重复候选集合不合法", ErrConflict)
		}
		available[candidateID] = struct{}{}
	}
	seenResolutions := make(map[string]struct{}, len(resolutions))
	for _, resolution := range resolutions {
		if resolution.Action != DuplicateKeepDistinct {
			return NewRuleError("invalid_duplicate_resolution", "重复候选决定不合法", ErrInvalidInput)
		}
		if _, duplicate := seenResolutions[resolution.CandidateID]; duplicate {
			return NewRuleError("duplicate_duplicate_resolution", "同一重复候选只能决定一次", ErrInvalidInput)
		}
		seenResolutions[resolution.CandidateID] = struct{}{}
		if _, exists := available[resolution.CandidateID]; !exists {
			return NewRuleError("invalid_duplicate_candidate", "重复候选不属于当前 Claim", ErrConflict)
		}
	}
	return nil
}

func validateVisualDocument(document VisualDocument) error {
	if document.ID == "" || len(document.Pages) == 0 || len(document.Pages) > 20 {
		return NewRuleError("invalid_visual_document", "视觉候选文档不合法", ErrInvalidInput)
	}
	seenPages := make(map[string]struct{}, len(document.Pages))
	for index, page := range document.Pages {
		if page.ID == "" || page.DocumentID != document.ID || page.PageNumber != index+1 {
			return NewRuleError("invalid_visual_document", "视觉候选页码或归属不合法", ErrInvalidInput)
		}
		if _, duplicate := seenPages[page.ID]; duplicate {
			return NewRuleError("invalid_visual_document", "视觉候选页面重复", ErrInvalidInput)
		}
		seenPages[page.ID] = struct{}{}
		if err := page.Fingerprint.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func visualDocumentsNear(left, right VisualDocument) (bool, int, int, error) {
	if len(left.Pages) != len(right.Pages) {
		return false, 0, 0, nil
	}
	maxDHash, maxAHash := 0, 0
	for index := range left.Pages {
		near, dhashDistance, ahashDistance, err := VisualPagesNear(left.Pages[index], right.Pages[index])
		if err != nil {
			return false, 0, 0, err
		}
		if !near {
			return false, 0, 0, nil
		}
		maxDHash = max(maxDHash, dhashDistance)
		maxAHash = max(maxAHash, ahashDistance)
	}
	return true, maxDHash, maxAHash, nil
}

func visualSignalLess(left, right VisualDuplicateSignal) bool {
	priority := map[string]int{"near_file": 0, "cross_page": 1}
	if priority[left.Kind] != priority[right.Kind] {
		return priority[left.Kind] < priority[right.Kind]
	}
	leftDistance := left.DHashDistance + left.AHashDistance
	rightDistance := right.DHashDistance + right.AHashDistance
	if leftDistance != rightDistance {
		return leftDistance < rightDistance
	}
	if left.ExistingDocumentID != right.ExistingDocumentID {
		return left.ExistingDocumentID < right.ExistingDocumentID
	}
	if left.CurrentPageNumber != right.CurrentPageNumber {
		return left.CurrentPageNumber < right.CurrentPageNumber
	}
	return left.ExistingPageNumber < right.ExistingPageNumber
}

func absoluteDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func parseVisualHash(value string) (uint64, error) {
	if len(value) != 16 || value != strings.ToLower(value) {
		return 0, NewRuleError("invalid_visual_fingerprint", "页面视觉哈希格式不合法", ErrInvalidInput)
	}
	parsed, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		return 0, NewRuleError("invalid_visual_fingerprint", "页面视觉哈希格式不合法", ErrInvalidInput)
	}
	return parsed, nil
}

func formatVisualHash(value uint64) string {
	return leftPadVisualHash(strconv.FormatUint(value, 16))
}

func leftPadVisualHash(value string) string {
	return strings.Repeat("0", 16-len(value)) + value
}
