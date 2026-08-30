package openaicompatible

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const maxSafeSchemaViolations = 3

const (
	schemaStageContentJSON      = "content_json"
	schemaStageProviderSchema   = "provider_schema"
	schemaStageExtractionSchema = "extraction_schema"
	schemaStageNormalizedJSON   = "normalized_json"
	schemaStageExtractionDecode = "extraction_decode"
)

var safeSchemaInstanceMembers = map[string]struct{}{
	"schema_version":     {},
	"document_type":      {},
	"payment":            {},
	"invoice":            {},
	"amount":             {},
	"currency":           {},
	"merchant":           {},
	"transaction_time":   {},
	"timezone":           {},
	"payment_method":     {},
	"order_number":       {},
	"category":           {},
	"invoice_number":     {},
	"invoice_date":       {},
	"amount_without_tax": {},
	"tax_amount":         {},
	"amount_with_tax":    {},
	"tax":                {},
	"seller_name":        {},
	"buyer_name":         {},
	"items":              {},
	"name":               {},
	"quantity":           {},
	"unit":               {},
	"unit_price":         {},
	"text":               {},
	"page":               {},
}

var safeSchemaKeywords = map[string]struct{}{
	"additionalProperties": {},
	"allOf":                {},
	"anyOf":                {},
	"const":                {},
	"dependentRequired":    {},
	"enum":                 {},
	"format":               {},
	"items":                {},
	"maxItems":             {},
	"maxLength":            {},
	"maximum":              {},
	"minItems":             {},
	"minLength":            {},
	"minimum":              {},
	"not":                  {},
	"oneOf":                {},
	"pattern":              {},
	"properties":           {},
	"required":             {},
	"type":                 {},
	"uniqueItems":          {},
}

func schemaProviderError(stage, message string, cause error, latency time.Duration) error {
	return &ports.ProviderCallError{
		Code:           "schema_validation_failed",
		DiagnosticCode: schemaDiagnosticCode(stage),
		SafeMessage:    message + "；" + safeSchemaFailureDetail(stage, cause),
		Retryable:      retryableSchemaFailure(stage),
		Latency:        latency,
		Cause:          cause,
	}
}

func retryableSchemaFailure(stage string) bool {
	switch stage {
	case schemaStageContentJSON, schemaStageProviderSchema, schemaStageExtractionSchema:
		return true
	default:
		return false
	}
}

func schemaDiagnosticCode(stage string) string {
	switch stage {
	case schemaStageContentJSON:
		return "provider_output_json_invalid"
	case schemaStageProviderSchema:
		return "provider_output_contract_invalid"
	case schemaStageExtractionSchema:
		return "bill_extraction_schema_invalid"
	case schemaStageNormalizedJSON:
		return "provider_output_normalization_failed"
	case schemaStageExtractionDecode:
		return "bill_extraction_decode_failed"
	default:
		return "schema_validation_failed"
	}
}

func safeSchemaFailureDetail(stage string, cause error) string {
	var syntaxError *json.SyntaxError
	if errors.As(cause, &syntaxError) {
		return "诊断[stage=" + stage + "; violations=/#json_syntax(offset=" + strconv.FormatInt(syntaxError.Offset, 10) + ")]"
	}

	var validationError *jsonschema.ValidationError
	if !errors.As(cause, &validationError) {
		return "诊断[stage=" + stage + "; violations=unavailable]"
	}

	violations := make([]string, 0, maxSafeSchemaViolations)
	collectSafeSchemaViolations(validationError, &violations)
	sort.Strings(violations)
	violations = compactStrings(violations)
	remaining := len(violations) - maxSafeSchemaViolations
	if remaining > 0 {
		violations = violations[:maxSafeSchemaViolations]
		violations = append(violations, "+"+strconv.Itoa(remaining))
	}
	if len(violations) == 0 {
		violations = append(violations, "/#schema")
	}
	return "诊断[stage=" + stage + "; violations=" + strings.Join(violations, ",") + "]"
}

func collectSafeSchemaViolations(validationError *jsonschema.ValidationError, result *[]string) {
	if len(validationError.Causes) != 0 {
		for _, cause := range validationError.Causes {
			collectSafeSchemaViolations(cause, result)
		}
		return
	}
	keyword := safeSchemaKeyword(validationError.ErrorKind.KeywordPath())
	violation := safeSchemaInstancePointer(validationError.InstanceLocation) + "#" + keyword
	if required, ok := validationError.ErrorKind.(*kind.Required); ok {
		missing := make([]string, 0, len(required.Missing))
		for _, name := range required.Missing {
			if _, safe := safeSchemaInstanceMembers[name]; safe {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) != 0 {
			violation += "(" + strings.Join(missing, "+") + ")"
		}
	}
	*result = append(*result, violation)
}

func safeSchemaKeyword(path []string) string {
	for index := len(path) - 1; index >= 0; index-- {
		if _, safe := safeSchemaKeywords[path[index]]; safe {
			return path[index]
		}
	}
	return "schema"
}

func safeSchemaInstancePointer(path []string) string {
	if len(path) == 0 {
		return "/"
	}
	segments := make([]string, 0, len(path))
	for _, segment := range path {
		if _, safe := safeSchemaInstanceMembers[segment]; safe {
			segments = append(segments, segment)
			continue
		}
		if index, err := strconv.Atoi(segment); err == nil && index >= 0 && index <= 9999 {
			segments = append(segments, segment)
			continue
		}
		segments = append(segments, "_")
	}
	return "/" + strings.Join(segments, "/")
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
