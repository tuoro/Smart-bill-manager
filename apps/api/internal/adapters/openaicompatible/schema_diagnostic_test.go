package openaicompatible

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestSchemaDiagnosticRetriesOnlyModelOutputValidationStages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stage     string
		retryable bool
	}{
		{stage: schemaStageContentJSON, retryable: true},
		{stage: schemaStageProviderSchema, retryable: true},
		{stage: schemaStageExtractionSchema, retryable: true},
		{stage: schemaStageNormalizedJSON, retryable: false},
		{stage: schemaStageExtractionDecode, retryable: false},
		{stage: "unknown", retryable: false},
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			err := schemaProviderError(test.stage, "安全错误", errors.New("synthetic cause"), time.Millisecond)
			callError := diagnosticProviderError(t, err, schemaDiagnosticCode(test.stage))
			if callError.Retryable != test.retryable {
				t.Fatalf("stage %s retryable = %v, want %v", test.stage, callError.Retryable, test.retryable)
			}
		})
	}
}

func TestSchemaDiagnosticReportsOnlySafeProviderContractLocations(t *testing.T) {
	t.Parallel()

	instance := providerPaymentExtraction()
	instance["schema_version"] = []any{"private-model-value"}
	prepared := preparedForTransport(t, completionTransport(t, instance))
	_, err := prepared.Execute(context.Background())
	callError := diagnosticProviderError(t, err, "provider_output_contract_invalid")
	if !strings.Contains(callError.SafeMessage, "stage=provider_schema") ||
		!strings.Contains(callError.SafeMessage, "/schema_version#") {
		t.Fatalf("safe diagnostic = %q", callError.SafeMessage)
	}
	for _, secret := range []string{"private-model-value"} {
		if strings.Contains(callError.SafeMessage, secret) {
			t.Fatalf("safe diagnostic leaked model content: %q", callError.SafeMessage)
		}
	}
}

func TestSchemaDiagnosticReportsWhitelistedMissingMembers(t *testing.T) {
	t.Parallel()

	instance := providerPaymentExtraction()
	delete(instance, "invoice")
	prepared := preparedForTransport(t, completionTransport(t, instance))
	_, err := prepared.Execute(context.Background())
	callError := diagnosticProviderError(t, err, "provider_output_contract_invalid")
	if !strings.Contains(callError.SafeMessage, "/#required(invoice)") {
		t.Fatalf("safe diagnostic = %q", callError.SafeMessage)
	}
}

func TestSchemaDiagnosticRedactsUntrustedPropertyNames(t *testing.T) {
	t.Parallel()

	instance := map[string]any{
		"schema_version":         "bill-visible-text/1",
		"document_type":          "unknown",
		"payment":                nil,
		"invoice":                nil,
		"private-account-number": "should-never-appear",
	}
	prepared := preparedForTransport(t, completionTransport(t, instance))
	_, err := prepared.Execute(context.Background())
	callError := diagnosticProviderError(t, err, "provider_output_contract_invalid")
	if !strings.Contains(callError.SafeMessage, "#additionalProperties") {
		t.Fatalf("safe diagnostic = %q", callError.SafeMessage)
	}
	if strings.Contains(callError.SafeMessage, "private-account-number") || strings.Contains(callError.SafeMessage, "should-never-appear") {
		t.Fatalf("safe diagnostic leaked an untrusted property: %q", callError.SafeMessage)
	}
}

func TestSchemaDiagnosticClassifiesInvalidContentJSONWithoutEchoingIt(t *testing.T) {
	t.Parallel()

	body := `{"choices":[{"message":{"content":"{private-model-content"}}]}`
	prepared := preparedForTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Request:    request,
		}, nil
	}))
	_, err := prepared.Execute(context.Background())
	callError := diagnosticProviderError(t, err, "provider_output_json_invalid")
	if !strings.Contains(callError.SafeMessage, "stage=content_json") ||
		!strings.Contains(callError.SafeMessage, "#json_syntax(offset=") {
		t.Fatalf("safe diagnostic = %q", callError.SafeMessage)
	}
	if strings.Contains(callError.SafeMessage, "private-model-content") {
		t.Fatalf("safe diagnostic leaked invalid JSON: %q", callError.SafeMessage)
	}
}

func diagnosticProviderError(t *testing.T, err error, diagnosticCode string) *ports.ProviderCallError {
	t.Helper()
	var callError *ports.ProviderCallError
	if !errors.As(err, &callError) || callError.Code != "schema_validation_failed" || callError.DiagnosticCode != diagnosticCode {
		t.Fatalf("provider error = %#v, want diagnostic %s", err, diagnosticCode)
	}
	return callError
}
