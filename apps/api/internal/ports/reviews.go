package ports

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type ReviewEvidence struct {
	ID             string
	DocumentPageID string
	Page           int
	Quote          string
	Region         json.RawMessage
}

type ReviewField struct {
	ID              string
	Path            string
	ValueType       string
	Presence        string
	Value           json.RawMessage
	NormalizedValue json.RawMessage
	Source          string
	SourceUserID    string
	Evidence        []ReviewEvidence
}

type ReviewValidation struct {
	ID           string
	FieldClaimID string
	RuleCode     string
	Severity     string
	Status       string
	SafeMessage  string
}

type LinkCandidate struct {
	ID               string
	TargetType       domain.DocumentType
	TargetID         string
	AmountMinor      int64
	AllocatedMinor   int64
	RemainingMinor   int64
	Currency         string
	BusinessDate     string
	DisplayName      string
	Available        bool
	NameExact        bool
	DateDistanceDays int
	ReasonCodes      []string
}

type DuplicateCandidate struct {
	ID                 string
	Kind               string
	ExistingDocumentID string
	ExistingPaymentID  string
	ExistingInvoiceID  string
	DisplayName        string
	BusinessDate       string
	AmountMinor        *int64
	CurrentPageNumber  *int
	ExistingPageNumber *int
	DHashDistance      *int
	AHashDistance      *int
	Available          bool
	ReasonCodes        []string
}

type ReviewSnapshot struct {
	Job                 JobSummary
	DocumentID          string
	PageCount           int
	ClaimSetID          string
	OriginAiRunID       string
	DocumentType        domain.DocumentType
	Revision            int
	OptimisticVersion   int
	Status              domain.ClaimStatus
	Fields              []ReviewField
	Validations         []ReviewValidation
	Candidates          []LinkCandidate
	DuplicateCandidates []DuplicateCandidate
	Pages               []domain.ClaimReviewPage
	InvoiceItemSpans    []domain.InvoiceItemPageSpan
}

type ReviewRepository interface {
	GetReview(ctx context.Context, tenantID, jobID string) (ReviewSnapshot, error)
	GetClaimSet(ctx context.Context, tenantID, claimSetID string) (ReviewSnapshot, error)
	GetConfirmReplay(ctx context.Context, tenantID, jobID, idempotencyKey string) (ConfirmReplay, error)
	GetRejectReplay(ctx context.Context, tenantID, jobID, idempotencyKey string) (RejectReplay, error)
}

type RevisionFieldRecord struct {
	FieldClaimRecord
	Source            string
	SourceUserID      string
	SupersedesFieldID string
}

type RevisionEvidenceRecord struct {
	EvidenceRecord
	CopiedFromEvidenceID string
}

type RevisionCommand struct {
	TenantID                   string
	JobID                      string
	DocumentID                 string
	PreviousClaimSetID         string
	ClaimSet                   ClaimSetRecord
	RevisedByUserID            string
	ExpectedRevision           int
	ExpectedOptimisticVersion  int
	Fields                     []RevisionFieldRecord
	Evidence                   []RevisionEvidenceRecord
	Validations                []ValidationRecord
	Candidates                 []LinkCandidateRecord
	DuplicateCandidates        []DuplicateCandidateRecord
	NormalizedInvoiceNumber    string
	DuplicateInvoiceValidation *ValidationRecord
}

type PaymentDraft struct {
	ID              string
	AmountMinor     int64
	Currency        string
	Merchant        string
	TransactionTime string
	SourceTimezone  string
	BusinessDate    string
	PaymentMethod   *string
	OrderNumber     *string
	Category        *string
}

type InvoiceItemDraft struct {
	ID             string
	ItemKey        string
	Name           string
	Quantity       *string
	Unit           *string
	UnitPriceMinor *int64
	AmountMinor    int64
	TaxMinor       *int64
	SortOrder      int
}

type InvoiceDraft struct {
	ID                      string
	InvoiceNumber           string
	NormalizedInvoiceNumber string
	InvoiceDate             string
	TotalMinor              int64
	TaxMinor                *int64
	Currency                string
	SellerName              string
	BuyerName               string
	Items                   []InvoiceItemDraft
}

type TripDraft struct {
	ID               string
	Origin           *string
	Destination      string
	StartDate        string
	EndDate          string
	TravelerName     *string
	TransportType    *string
	BookingReference *string
}

type FactOriginDraft struct {
	ID           string
	FieldPath    string
	FieldClaimID string
	FactScope    string
	ItemKey      string
}

type CandidateDecisionDraft struct {
	ID             string
	CandidateID    string
	Action         string
	AllocatedMinor *int64
	Currency       string
	LinkID         string
}

type DuplicateCandidateDecisionDraft struct {
	ID          string
	CandidateID string
	Action      string
}

type ConfirmCommand struct {
	TenantID           string
	JobID              string
	ClaimSetID         string
	ActorUserID        string
	ReviewDecisionID   string
	IdempotencyKey     string
	ExpectedRevision   int
	AssociationMode    string
	AllocationPlanHash string
	DuplicatePlanHash  string
	Payment            *PaymentDraft
	Invoice            *InvoiceDraft
	Trip               *TripDraft
	Origins            []FactOriginDraft
	CandidateDecisions []CandidateDecisionDraft
	DuplicateDecisions []DuplicateCandidateDecisionDraft
	AuditEventID       string
	RequestID          string
	CreatedAt          time.Time
}

type ConfirmResult struct {
	ReviewDecisionID string
	FactType         domain.DocumentType
	FactID           string
	LinkIDs          []string
	Replayed         bool
}

type ConfirmReplay struct {
	Result             ConfirmResult
	ClaimSetID         string
	ExpectedRevision   int
	AssociationMode    string
	AllocationPlanHash string
	DuplicatePlanHash  string
}

type RejectCommand struct {
	TenantID         string
	JobID            string
	ClaimSetID       string
	ActorUserID      string
	ReviewDecisionID string
	IdempotencyKey   string
	ExpectedRevision int
	Reason           string
	AuditEventID     string
	RequestID        string
	CreatedAt        time.Time
}

type RejectReplay struct {
	ClaimSetID       string
	ExpectedRevision int
	Reason           string
}

type FactDeleteCommand struct {
	TenantID     string
	FactType     domain.DocumentType
	FactID       string
	ActorUserID  string
	AuditEventID string
	RequestID    string
	DeletedAt    time.Time
}

type ReviewTransaction interface {
	PersistRevision(ctx context.Context, command RevisionCommand) error
	ConfirmReview(ctx context.Context, command ConfirmCommand) (ConfirmResult, error)
	RejectReview(ctx context.Context, command RejectCommand) error
}

type Payment struct {
	ID               string    `json:"id"`
	AmountMinor      int64     `json:"amount_minor"`
	AllocatedMinor   int64     `json:"allocated_minor"`
	RemainingMinor   int64     `json:"remaining_minor"`
	AllocationStatus string    `json:"allocation_status"`
	Currency         string    `json:"currency"`
	Merchant         string    `json:"merchant"`
	TransactionTime  string    `json:"transaction_time"`
	SourceTimezone   string    `json:"source_timezone"`
	PaymentMethod    *string   `json:"payment_method,omitempty"`
	OrderNumber      *string   `json:"order_number,omitempty"`
	Category         *string   `json:"category,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type InvoiceItem struct {
	ItemKey        string  `json:"item_key"`
	Name           string  `json:"name"`
	Quantity       *string `json:"quantity,omitempty"`
	Unit           *string `json:"unit,omitempty"`
	UnitPriceMinor *int64  `json:"unit_price_minor,omitempty"`
	AmountMinor    int64   `json:"amount_minor"`
	TaxMinor       *int64  `json:"tax_minor,omitempty"`
	SortOrder      int     `json:"sort_order"`
}

type Invoice struct {
	ID               string        `json:"id"`
	InvoiceNumber    string        `json:"invoice_number"`
	InvoiceDate      string        `json:"invoice_date"`
	TotalMinor       int64         `json:"total_minor"`
	AllocatedMinor   int64         `json:"allocated_minor"`
	RemainingMinor   int64         `json:"remaining_minor"`
	AllocationStatus string        `json:"allocation_status"`
	TaxMinor         *int64        `json:"tax_minor,omitempty"`
	Currency         string        `json:"currency"`
	SellerName       string        `json:"seller_name"`
	BuyerName        string        `json:"buyer_name"`
	Items            []InvoiceItem `json:"items"`
	CreatedAt        time.Time     `json:"created_at"`
}

type Trip struct {
	ID                   string    `json:"id"`
	Origin               *string   `json:"origin,omitempty"`
	Destination          string    `json:"destination"`
	StartDate            string    `json:"start_date"`
	EndDate              string    `json:"end_date"`
	TravelerName         *string   `json:"traveler_name,omitempty"`
	TransportType        *string   `json:"transport_type,omitempty"`
	BookingReference     *string   `json:"booking_reference,omitempty"`
	AssignedPaymentCount int       `json:"assigned_payment_count"`
	AssignedInvoiceCount int       `json:"assigned_invoice_count"`
	CreatedAt            time.Time `json:"created_at"`
}

type FactRepository interface {
	ListPayments(ctx context.Context, tenantID string) ([]Payment, error)
	ListInvoices(ctx context.Context, tenantID string) ([]Invoice, error)
	ListTrips(ctx context.Context, tenantID string) ([]Trip, error)
}
