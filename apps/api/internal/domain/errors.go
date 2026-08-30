package domain

import "errors"

var (
	ErrUnauthenticated   = errors.New("unauthenticated")
	ErrForbidden         = errors.New("forbidden")
	ErrNotFound          = errors.New("not_found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidInput      = errors.New("invalid_input")
	ErrBootstrapNotEmpty = errors.New("bootstrap_not_empty")
	ErrTenantRequired    = errors.New("tenant_required")
	ErrVersionConflict   = errors.New("version_conflict")
	ErrPayloadTooLarge   = errors.New("payload_too_large")
	ErrUnavailable       = errors.New("unavailable")
)

type RuleError struct {
	Code    string
	Message string
	Cause   error
}

func (e *RuleError) Error() string {
	return e.Code + ": " + e.Message
}

func (e *RuleError) Unwrap() error {
	return e.Cause
}

func NewRuleError(code, message string, cause error) error {
	return &RuleError{Code: code, Message: message, Cause: cause}
}

type DuplicateDocumentError struct {
	DocumentID string
}

func (e *DuplicateDocumentError) Error() string {
	return "duplicate_document"
}

func (e *DuplicateDocumentError) Unwrap() error {
	return ErrConflict
}
