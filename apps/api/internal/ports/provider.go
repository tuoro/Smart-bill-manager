package ports

import (
	"context"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

const (
	ProviderOutputModeJSONSchema = "json_schema"
	ProviderOutputModeJSONObject = "json_object"
)

type SecretCipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
	Fingerprint(parts ...[]byte) string
}

type ProviderConfig struct {
	ID                      string
	TenantID                string
	BaseURL                 string
	EncryptedAPIKey         []byte
	Model                   string
	OutputMode              string
	CapabilityStatus        string
	CapabilityCheckedAt     *time.Time
	CapabilitySafeMessage   string
	CapabilitySchemaVersion string
	CapabilitySchemaSHA256  string
	Active                  bool
	Version                 int
	SafeFingerprint         string
	CreatedByUserID         string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type ProviderCredentials struct {
	BaseURL                 string
	APIKey                  []byte
	Model                   string
	OutputMode              string
	Version                 int
	CapabilitySchemaVersion string
	CapabilitySchemaSHA256  string
}

type ProviderSchemaIdentity struct {
	Version string
	SHA256  string
}

type CapabilityResult struct {
	Passed      bool
	SafeMessage string
}

type ProviderDeleteCommand struct {
	TenantID        string
	ConfigID        string
	ActorUserID     string
	AuditEventID    string
	RequestID       string
	ExpectedVersion int
	DeletedAt       time.Time
}

type ProviderRepository interface {
	ListProviderConfigs(ctx context.Context, tenantID string) ([]ProviderConfig, error)
	GetProviderConfig(ctx context.Context, tenantID, configID string) (ProviderConfig, error)
}

type ProviderDetector interface {
	ProviderSchemaIdentity() ProviderSchemaIdentity
	DetectCapabilities(ctx context.Context, credentials ProviderCredentials) CapabilityResult
}

type PageImage struct {
	PageNumber int
	MIME       string
	Data       []byte
	SHA256     string
}

type BillVisibleTextEnvelope = domain.BillVisibleTextEnvelope

type BillExtractionResult struct {
	Envelope     BillVisibleTextEnvelope
	ResponseHash string
	InputTokens  int
	OutputTokens int
	Latency      time.Duration
}

type PreparedBillExtraction interface {
	RequestHash() string
	ProviderSchemaIdentity() ProviderSchemaIdentity
	Execute(ctx context.Context) (BillExtractionResult, error)
}

type BillExtractor interface {
	ProviderSchemaIdentity() ProviderSchemaIdentity
	Prepare(credentials ProviderCredentials, pages []PageImage) (PreparedBillExtraction, error)
}

type ProviderCallError struct {
	Code           string
	DiagnosticCode string
	SafeMessage    string
	Retryable      bool
	Latency        time.Duration
	Cause          error
}

func (e *ProviderCallError) Error() string {
	return e.Code + ": " + e.SafeMessage
}

func (e *ProviderCallError) Unwrap() error {
	return e.Cause
}
