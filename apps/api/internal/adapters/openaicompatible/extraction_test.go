package openaicompatible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestBillExtractionUsesFrozenRequestAndValidatesDirectBusinessResponse(t *testing.T) {
	t.Parallel()
	detector := testDetector(t)
	detector.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte("data:image/png;base64,")) || bytes.Contains(body, []byte(`"tools"`)) {
			t.Fatalf("invalid bill extraction request: %s", body)
		}
		if bytes.Contains(body, []byte(`"uniqueItems"`)) || bytes.Contains(body, []byte(`"allOf"`)) {
			t.Fatalf("request retained local-only schema keywords: %s", body)
		}
		assertBillExtractionRequestContract(t, body)
		return completionResponse(t, request, providerPaymentExtraction(), 10, 8), nil
	})}
	prepared, err := detector.Prepare(
		testProviderCredentials(detector),
		[]ports.PageImage{{PageNumber: 1, MIME: "image/png", Data: []byte("synthetic-image"), SHA256: "hash"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.RequestHash()) != 64 {
		t.Fatalf("request hash = %q", prepared.RequestHash())
	}
	result, err := prepared.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payment map[string]any
	if err := json.Unmarshal(result.Envelope.Payment, &payment); err != nil {
		t.Fatal(err)
	}
	amount := payment["amount"].(map[string]any)
	if result.Envelope.DocumentType != "payment" || result.InputTokens != 10 || result.OutputTokens != 8 ||
		amount["text"] != "CNY 12.34" || amount["page"] != float64(1) {
		t.Fatalf("result = %#v", result)
	}
}

func assertBillExtractionRequestContract(t *testing.T, body []byte) {
	t.Helper()
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode bill extraction request: %v", err)
	}
	messages := request["messages"].([]any)
	system := messages[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "bill-visible-text-cn/1") || strings.Contains(system, "bill-extract/2") {
		t.Fatalf("system instruction does not pin direct extraction contract: %q", system)
	}
	if temperature, ok := request["temperature"].(float64); !ok || temperature != 0 {
		t.Fatalf("temperature = %#v, want deterministic zero", request["temperature"])
	}
	content := messages[1].(map[string]any)["content"].([]any)
	instruction := content[0].(map[string]any)["text"].(string)
	for _, required := range []string{
		"任务版本：bill-visible-text-cn/1",
		`"schema_version": "bill-visible-text/1"`,
		`{"text":"只含值本身的原文","page":1}`,
		"只抄我们需要的票面原文",
		"不解释、不计算、不猜测、不纠错",
		`"amount_without_tax"`,
		`"amount_with_tax"`,
		"空白单价、金额或税额必须是 null",
		"价税合计（小写）",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("bill extraction instruction is missing %q", required)
		}
	}
	if strings.Contains(instruction, "amount_minor") || strings.Contains(instruction, "value_minor") {
		t.Fatal("model-facing instruction exposes internal minor-unit fields")
	}
	responseFormat := request["response_format"].(map[string]any)
	if responseFormat["type"] != "json_schema" {
		t.Fatalf("response format type = %#v", responseFormat["type"])
	}
	jsonSchema := responseFormat["json_schema"].(map[string]any)
	if jsonSchema["name"] != "bill_visible_text_provider" {
		t.Fatalf("schema name = %#v", jsonSchema["name"])
	}
	providerSchema := jsonSchema["schema"].(map[string]any)
	properties := providerSchema["properties"].(map[string]any)
	for _, key := range []string{"schema_version", "document_type", "payment", "invoice"} {
		if _, exists := properties[key]; !exists {
			t.Fatalf("Provider root schema is missing %s", key)
		}
	}
	encoded, err := json.Marshal(providerSchema)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("amount_minor")) || bytes.Contains(encoded, []byte("value_minor")) {
		t.Fatal("Provider schema exposes internal minor-unit fields")
	}
}

func TestBillExtractionUsesExplicitJSONObjectModeAndLocalSchema(t *testing.T) {
	t.Parallel()
	detector := testDetector(t)
	detector.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		responseFormat := payload["response_format"].(map[string]any)
		if len(responseFormat) != 1 || responseFormat["type"] != "json_object" {
			t.Fatalf("JSON object response format = %#v", responseFormat)
		}
		messages := payload["messages"].([]any)
		system := messages[0].(map[string]any)["content"].(string)
		if strings.Contains(system, `"$defs"`) {
			t.Fatal("JSON Object bill extraction request duplicated the Provider schema")
		}
		return completionResponse(t, request, providerUnknownExtraction(), 0, 0), nil
	})}
	credentials := testProviderCredentials(detector)
	credentials.OutputMode = ports.ProviderOutputModeJSONObject
	prepared, err := detector.Prepare(credentials, []ports.PageImage{{PageNumber: 1, MIME: "image/png", Data: []byte("image")}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Envelope.DocumentType != "unknown" || string(result.Envelope.Payment) != "null" || string(result.Envelope.Invoice) != "null" {
		t.Fatalf("unknown extraction = %#v", result.Envelope)
	}
}

func TestBillExtractionStrictModeRejectsMissingRootMemberWithoutRepair(t *testing.T) {
	t.Parallel()
	instance := providerPaymentExtraction()
	delete(instance, "invoice")
	prepared := preparedForTransport(t, completionTransport(t, instance))
	_, err := prepared.Execute(context.Background())
	assertProviderError(t, err, "schema_validation_failed", true)
}

func TestBillExtractionJSONObjectModePreservesMalformedVisibleFieldsForClaimValidation(t *testing.T) {
	t.Parallel()
	detector := testDetector(t)
	instance := providerPaymentExtraction()
	instance["payment"].(map[string]any)["amount"] = map[string]any{"unexpected": true}
	detector.client = &http.Client{Transport: completionTransport(t, instance)}
	credentials := testProviderCredentials(detector)
	credentials.OutputMode = ports.ProviderOutputModeJSONObject
	prepared, err := detector.Prepare(credentials, []ports.PageImage{{PageNumber: 1, MIME: "image/png", Data: []byte("image")}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Envelope.Payment, []byte("unexpected")) {
		t.Fatalf("field-level data was lost: %#v", result.Envelope)
	}
}

func TestBillExtractionPreservesNestedContractViolationForLocalValidation(t *testing.T) {
	t.Parallel()
	instance := providerPaymentExtraction()
	instance["payment"].(map[string]any)["amount"] = 12.34
	prepared := preparedForTransport(t, completionTransport(t, instance))
	result, err := prepared.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Envelope.Payment, []byte(`"amount":12.34`)) {
		t.Fatalf("nested invalid field was not preserved: %s", result.Envelope.Payment)
	}
}

func TestBillExtractionClassifiesProviderAndTransportFailures(t *testing.T) {
	t.Parallel()
	statusCases := []struct {
		status    int
		code      string
		retryable bool
	}{
		{status: http.StatusUnauthorized, code: "provider_auth_failed"},
		{status: http.StatusForbidden, code: "provider_auth_failed"},
		{status: http.StatusTooManyRequests, code: "provider_rate_limited", retryable: true},
		{status: http.StatusInternalServerError, code: "provider_unavailable", retryable: true},
		{status: http.StatusBadRequest, code: "provider_invalid_response"},
	}
	for _, test := range statusCases {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			prepared := preparedForTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: request}, nil
			}))
			_, err := prepared.Execute(context.Background())
			assertProviderError(t, err, test.code, test.retryable)
		})
	}
	for name, transportErr := range map[string]error{
		"timeout": context.DeadlineExceeded,
		"network": errors.New("synthetic network interruption"),
	} {
		t.Run(name, func(t *testing.T) {
			prepared := preparedForTransport(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, transportErr
			}))
			_, err := prepared.Execute(context.Background())
			if name == "timeout" {
				assertProviderError(t, err, "provider_timeout", false)
			} else {
				assertProviderError(t, err, "provider_unavailable", true)
			}
		})
	}
	for _, test := range []struct {
		name      string
		body      string
		code      string
		retryable bool
	}{
		{name: "outer-json", body: `{`, code: "provider_invalid_response"},
		{name: "missing-choice", body: `{}`, code: "provider_invalid_response"},
		{name: "content-json", body: `{"choices":[{"message":{"content":"{"}}]}`, code: "schema_validation_failed", retryable: true},
		{name: "schema", body: `{"choices":[{"message":{"content":"{}"}}]}`, code: "schema_validation_failed", retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared := preparedForTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(test.body)), Request: request}, nil
			}))
			_, err := prepared.Execute(context.Background())
			assertProviderError(t, err, test.code, test.retryable)
		})
	}
}

func TestBillExtractionPreparationBoundaries(t *testing.T) {
	t.Parallel()
	detector := testDetector(t)
	credentials := testProviderCredentials(detector)
	validPage := ports.PageImage{PageNumber: 1, MIME: "image/png", Data: []byte("image")}
	for _, test := range []struct {
		name        string
		credentials ports.ProviderCredentials
		pages       []ports.PageImage
	}{
		{name: "missing-credentials", credentials: ports.ProviderCredentials{}, pages: []ports.PageImage{validPage}},
		{name: "unsupported-output-mode", credentials: func() ports.ProviderCredentials {
			invalid := credentials
			invalid.OutputMode = "unsupported"
			return invalid
		}(), pages: []ports.PageImage{validPage}},
		{name: "no-pages", credentials: credentials},
		{name: "too-many-pages", credentials: credentials, pages: make([]ports.PageImage, 21)},
		{name: "non-contiguous", credentials: credentials, pages: []ports.PageImage{{PageNumber: 2, MIME: "image/png", Data: []byte("image")}}},
		{name: "unsupported-mime", credentials: credentials, pages: []ports.PageImage{{PageNumber: 1, MIME: "application/pdf", Data: []byte("image")}}},
		{name: "empty-page", credentials: credentials, pages: []ports.PageImage{{PageNumber: 1, MIME: "image/png"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := detector.Prepare(test.credentials, test.pages); err == nil {
				t.Fatalf("invalid preparation %s accepted", test.name)
			}
		})
	}
}

func preparedForTransport(t *testing.T, transport http.RoundTripper) ports.PreparedBillExtraction {
	t.Helper()
	detector := testDetector(t)
	detector.client = &http.Client{Transport: transport}
	prepared, err := detector.Prepare(
		testProviderCredentials(detector),
		[]ports.PageImage{{PageNumber: 1, MIME: "image/png", Data: []byte("image")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func testProviderCredentials(detector *Detector) ports.ProviderCredentials {
	identity := detector.ProviderSchemaIdentity()
	return ports.ProviderCredentials{
		BaseURL:                 "https://provider.example.test/v1",
		APIKey:                  []byte("key"),
		Model:                   "model",
		OutputMode:              ports.ProviderOutputModeJSONSchema,
		Version:                 1,
		CapabilitySchemaVersion: identity.Version,
		CapabilitySchemaSHA256:  identity.SHA256,
	}
}

func providerPaymentExtraction() map[string]any {
	return map[string]any{
		"schema_version": "bill-visible-text/1",
		"document_type":  "payment",
		"payment": map[string]any{
			"amount":           map[string]any{"text": "CNY 12.34", "page": 1},
			"currency":         map[string]any{"text": "CNY", "page": 1},
			"merchant":         map[string]any{"text": "Synthetic Merchant", "page": 1},
			"transaction_time": map[string]any{"text": "2026-08-29 14:35", "page": 1},
			"timezone":         nil,
			"payment_method":   nil,
			"order_number":     nil,
			"category":         nil,
		},
		"invoice": nil,
	}
}

func providerInvoiceExtraction() map[string]any {
	return map[string]any{
		"schema_version": "bill-visible-text/1",
		"document_type":  "invoice",
		"payment":        nil,
		"invoice": map[string]any{
			"invoice_number":     map[string]any{"text": "000123", "page": 1},
			"invoice_date":       map[string]any{"text": "2026-08-29", "page": 1},
			"amount_without_tax": map[string]any{"text": "94.34", "page": 1},
			"tax_amount":         map[string]any{"text": "5.66", "page": 1},
			"amount_with_tax":    map[string]any{"text": "100.00", "page": 1},
			"currency":           map[string]any{"text": "CNY", "page": 1},
			"seller_name":        map[string]any{"text": "销售方有限公司", "page": 1},
			"buyer_name":         map[string]any{"text": "购买方有限公司", "page": 1},
			"items":              []any{},
		},
	}
}

func providerUnknownExtraction() map[string]any {
	return map[string]any{
		"schema_version": "bill-visible-text/1",
		"document_type":  "unknown",
		"payment":        nil,
		"invoice":        nil,
	}
}

func completionTransport(t *testing.T, instance any) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return completionResponse(t, request, instance, 0, 0), nil
	})
}

func completionResponse(t *testing.T, request *http.Request, instance any, inputTokens, outputTokens int) *http.Response {
	t.Helper()
	content, err := json.Marshal(instance)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}},
		"usage":   map[string]any{"prompt_tokens": inputTokens, "completion_tokens": outputTokens},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}

func assertProviderError(t *testing.T, err error, code string, retryable bool) {
	t.Helper()
	var callError *ports.ProviderCallError
	if !errors.As(err, &callError) || callError.Code != code || callError.Retryable != retryable {
		t.Fatalf("provider error = %#v, want %s retryable=%v", err, code, retryable)
	}
}
