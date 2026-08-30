package ports

import (
	"context"
	"io"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

const MaxDocumentBytes int64 = 20 * 1024 * 1024
const MaxDerivedPageBytes int64 = 64 * 1024 * 1024

type StagedObject struct {
	ID     string
	Size   int64
	SHA256 string
}

type ObjectStore interface {
	Stage(ctx context.Context, source io.Reader, maxBytes int64) (StagedObject, error)
	Commit(ctx context.Context, staged StagedObject, storageKey string) error
	Abort(ctx context.Context, staged StagedObject) error
	Open(ctx context.Context, storageKey string) (io.ReadCloser, error)
	Delete(ctx context.Context, storageKey string) error
}

type RecoverableDeletionStore interface {
	StageDeletion(ctx context.Context, deletionID string, storageKeys []string) error
	RestoreDeletion(ctx context.Context, deletionID string) error
	PurgeDeletion(ctx context.Context, deletionID string) error
	PendingDeletions(ctx context.Context) ([]string, error)
}

type DocumentInspection struct {
	DetectedMIME string
	PageCount    int
	Width        int
	Height       int
}

type DocumentInspector interface {
	InspectStaged(
		ctx context.Context,
		staged StagedObject,
		originalName string,
		declaredMIME string,
	) (DocumentInspection, error)
}

type ProcessingDocument struct {
	TenantID   string
	DocumentID string
	StorageKey string
	MIME       string
	PageCount  int
}

type NormalizedPage struct {
	ID string
	PageImage
	StorageKey        string
	Width             int
	Height            int
	VisualFingerprint domain.PageVisualFingerprint
}

type DocumentNormalizer interface {
	Normalize(ctx context.Context, document ProcessingDocument) ([]NormalizedPage, error)
	DeleteNormalized(ctx context.Context, pages []NormalizedPage) error
}

type Document struct {
	ID              string
	TenantID        string
	StorageKey      string
	OriginalName    string
	DeclaredMIME    string
	DetectedMIME    string
	SizeBytes       int64
	SHA256          string
	PageCount       int
	Status          string
	CreatedByUserID string
	CreatedAt       time.Time
}

type ProcessingJob struct {
	ID           string
	TenantID     string
	DocumentID   string
	Kind         string
	Status       domain.JobStatus
	AttemptCount int
	CreatedAt    time.Time
	Version      int
}

type JobSummary struct {
	ID               string
	DocumentID       string
	OriginalName     string
	DetectedMIME     string
	Status           domain.JobStatus
	AttemptCount     int
	ErrorCode        string
	SafeErrorMessage string
	CreatedAt        time.Time
	Version          int
}

type Transaction interface {
	ProcessingTransaction
	ReviewTransaction
	RequestJobCancellation(
		ctx context.Context,
		tenantID, jobID, actorUserID, decisionID, idempotencyKey string,
		now time.Time,
	) error
	RetryJob(ctx context.Context, tenantID, jobID string) error
	FindDocumentIDBySHA(ctx context.Context, tenantID, sha256 string) (string, error)
	InsertDocument(ctx context.Context, document Document) error
	InsertProcessingJob(ctx context.Context, job ProcessingJob) error
	DeleteUnconfirmedDocument(ctx context.Context, tenantID, documentID string) error
	InsertProviderConfig(ctx context.Context, config ProviderConfig) error
	RecordProviderCapability(
		ctx context.Context,
		tenantID, configID string,
		expectedVersion int,
		status, safeMessage string,
		providerSchema ProviderSchemaIdentity,
		checkedAt time.Time,
	) error
	ActivateProviderConfig(
		ctx context.Context,
		tenantID, configID string,
		expectedVersion int,
		providerSchema ProviderSchemaIdentity,
		now time.Time,
	) error
	DeleteProviderConfig(ctx context.Context, command ProviderDeleteCommand) error
	DeleteFact(ctx context.Context, command FactDeleteCommand) error
	DeleteDocumentAggregate(ctx context.Context, command DocumentDeleteCommand) error
}

type TransactionManager interface {
	WithinTransaction(ctx context.Context, operation func(Transaction) error) error
}

type JobRepository interface {
	ListJobs(ctx context.Context, tenantID string, status *domain.JobStatus) ([]JobSummary, error)
	GetJob(ctx context.Context, tenantID, jobID string) (JobSummary, error)
}

type DocumentObject struct {
	StorageKey  string
	Name        string
	MIME        string
	ReviewState domain.JobStatus
}

type DocumentDeletionPlan struct {
	DocumentID     string
	StorageKeys    []string
	ObjectHashes   []string
	ResourceCounts map[string]int
}

type DocumentDeleteCommand struct {
	TenantID           string
	DocumentID         string
	ActorUserID        string
	TombstoneID        string
	ResourceIDHash     string
	ObjectHashesJSON   string
	ResourceCountsJSON string
	RequestID          string
	DeletedAt          time.Time
}

type DocumentRepository interface {
	GetDocument(ctx context.Context, tenantID, documentID string) (Document, error)
	GetDocumentObject(ctx context.Context, tenantID, documentID string) (DocumentObject, error)
}

type DocumentDeletionRepository interface {
	PrepareUnconfirmedDocumentDeletion(ctx context.Context, tenantID, documentID string) (DocumentDeletionPlan, error)
	DeletionTombstoneExists(ctx context.Context, tombstoneID string) (bool, error)
}
