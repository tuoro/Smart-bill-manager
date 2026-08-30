package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type LeasedJob struct {
	ID           string
	TenantID     string
	DocumentID   string
	StorageKey   string
	MIME         string
	PageCount    int
	AttemptCount int
	LeaseOwner   string
	LeaseExpires time.Time
}

type JobQueue interface {
	LeaseNextJob(ctx context.Context, workerID string, now, leaseExpires time.Time) (LeasedJob, error)
	CancellationRequested(ctx context.Context, tenantID, jobID string) (bool, error)
	GetDocumentPages(ctx context.Context, tenantID, documentID string) ([]NormalizedPage, error)
}

type ActiveProviderRepository interface {
	GetActiveProviderConfig(ctx context.Context, tenantID string) (ProviderConfig, error)
}

type DocumentPageRecord struct {
	ID                string
	TenantID          string
	DocumentID        string
	PageNumber        int
	StorageKey        string
	Width             int
	Height            int
	SHA256            string
	ProcessingVersion string
	VisualFingerprint domain.PageVisualFingerprint
	CreatedAt         time.Time
}

type AiRun struct {
	ID                        string
	TenantID                  string
	JobID                     string
	ProviderConfigID          string
	ProviderConfigVersion     int
	ProviderConfigFingerprint string
	Model                     string
	PromptVersion             string
	ExtractionSchemaVersion   string
	ProviderSchemaVersion     string
	ProviderSchemaSHA256      string
	ClaimSchemaVersion        string
	ClaimMapperVersion        string
	InputProcessingVersion    string
	RequestHash               string
	Outcome                   string
	StartedAt                 time.Time
}

type AiRunCompletion struct {
	TenantID     string
	AiRunID      string
	Outcome      string
	ResponseHash string
	InputTokens  int
	OutputTokens int
	Latency      time.Duration
	ErrorCode    string
	FinishedAt   time.Time
}

type ClaimSetRecord struct {
	ID            string
	TenantID      string
	DocumentID    string
	OriginAiRunID string
	DocumentType  domain.DocumentType
	Status        domain.ClaimStatus
	CreatedAt     time.Time
}

type FieldClaimRecord struct {
	ID              string
	TenantID        string
	ClaimSetID      string
	FieldPath       string
	ValueType       string
	Presence        string
	TypedValueJSON  string
	NormalizedValue string
	CreatedAt       time.Time
}

type EvidenceRecord struct {
	ID             string
	TenantID       string
	FieldClaimID   string
	DocumentPageID string
	Quote          string
	RegionJSON     string
	EvidenceHash   string
	CreatedAt      time.Time
}

type ValidationRecord struct {
	ID           string
	TenantID     string
	AiRunID      string
	ClaimSetID   string
	FieldClaimID string
	RuleCode     string
	Severity     string
	Status       string
	SafeMessage  string
	RuleVersion  string
	CreatedAt    time.Time
}

type ClaimBundle struct {
	ClaimSet            ClaimSetRecord
	Fields              []FieldClaimRecord
	Evidence            []EvidenceRecord
	Validations         []ValidationRecord
	Candidates          []LinkCandidateRecord
	DuplicateCandidates []DuplicateCandidateRecord
}

type LinkTarget struct {
	DocumentType   domain.DocumentType
	FactID         string
	AmountMinor    int64
	AllocatedMinor int64
	RemainingMinor int64
	Currency       string
	BusinessDate   string
	DisplayName    string
}

type LinkCandidateRecord struct {
	ID                string
	TenantID          string
	ClaimSetID        string
	ExistingPaymentID string
	ExistingInvoiceID string
	CandidateKey      string
	RuleVersion       string
	ReasonCodesJSON   string
	NameExact         bool
	DateDistanceDays  int
	CreatedAt         time.Time
}

type DuplicateCandidateRecord struct {
	ID                     string
	TenantID               string
	ClaimSetID             string
	Kind                   string
	ExistingDocumentID     string
	CurrentDocumentPageID  string
	ExistingDocumentPageID string
	ExistingPaymentID      string
	ExistingInvoiceID      string
	CandidateKey           string
	RuleVersion            string
	ReasonCodesJSON        string
	DHashDistance          *int
	AHashDistance          *int
	CreatedAt              time.Time
}

type ProcessingTransaction interface {
	InsertDocumentPages(ctx context.Context, pages []DocumentPageRecord) error
	InsertAiRun(ctx context.Context, run AiRun) error
	CompleteAiRun(ctx context.Context, completion AiRunCompletion) error
	InsertAiRunValidation(ctx context.Context, validation ValidationRecord) error
	IncrementJobAttempt(ctx context.Context, tenantID, jobID string) error
	InvoiceNumberExists(ctx context.Context, tenantID, normalizedInvoiceNumber string) (bool, error)
	ListEligibleLinkTargets(
		ctx context.Context,
		tenantID string,
		documentType domain.DocumentType,
		currency string,
	) ([]LinkTarget, error)
	ListVisualDuplicateDocuments(
		ctx context.Context,
		tenantID, documentID string,
	) (domain.VisualDocument, []domain.VisualDocument, error)
	ListFieldDuplicateTargets(
		ctx context.Context,
		tenantID string,
		input domain.FieldDuplicateInput,
	) ([]domain.FieldDuplicateTarget, error)
	PersistInitialClaim(ctx context.Context, jobID string, bundle ClaimBundle) error
	MarkJobFailed(ctx context.Context, tenantID, jobID, code, safeMessage string, finishedAt time.Time) error
	MarkJobCancelled(ctx context.Context, tenantID, jobID string, finishedAt time.Time) error
}
