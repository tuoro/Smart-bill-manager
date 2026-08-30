package openaicompatible

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestDetectorValidatesVisionAndStructuredOutput(t *testing.T) {
	t.Parallel()

	detector := testDetector(t)
	detector.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer synthetic-key" {
			t.Error("authorization header missing")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		responseFormat, ok := body["response_format"].(map[string]any)
		if body["model"] != "synthetic-model" || !ok || responseFormat["type"] != "json_schema" || body["temperature"] != float64(0) {
			t.Errorf("unexpected request body: %#v", body)
		}
		responseBody := []byte(`{
			"choices":[{"message":{"content":"{\"schema_version\":\"bill-visible-text/2\",\"document_type\":\"unknown\",\"payment\":null,\"invoice\":null,\"trip\":null}"}}]
		}`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}
	result := detector.DetectCapabilities(context.Background(), ports.ProviderCredentials{
		BaseURL:    "https://provider.example.test/v1",
		APIKey:     []byte("synthetic-key"),
		Model:      "synthetic-model",
		OutputMode: ports.ProviderOutputModeJSONSchema,
		Version:    1,
	})
	if !result.Passed {
		t.Fatalf("detection failed: %s", result.SafeMessage)
	}
}

func TestDetectorUsesExplicitJSONObjectMode(t *testing.T) {
	t.Parallel()

	detector := testDetector(t)
	detector.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		responseFormat, ok := body["response_format"].(map[string]any)
		if !ok || len(responseFormat) != 1 || responseFormat["type"] != "json_object" {
			t.Errorf("JSON object response format = %#v", responseFormat)
		}
		messages := body["messages"].([]any)
		system := messages[0].(map[string]any)["content"].(string)
		if !strings.Contains(system, jsonObjectSchemaInstruction) || !strings.Contains(system, `"payment"`) {
			t.Errorf("JSON Object capability probe is missing the Provider-facing Schema: %q", system)
		}
		content := messages[1].(map[string]any)["content"].([]any)
		instruction := content[0].(map[string]any)["text"].(string)
		if !strings.Contains(instruction, `"payment":null`) || !strings.Contains(instruction, `"trip":null`) || !strings.Contains(instruction, `"bill-visible-text/2"`) {
			t.Errorf("JSON Object capability probe is missing its compact visual contract: %q", instruction)
		}
		responseBody := []byte(`{
			"choices":[{"message":{"content":"{\"schema_version\":\"bill-visible-text/2\",\"document_type\":\"unknown\",\"payment\":null,\"invoice\":null,\"trip\":null}"}}]
		}`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}
	result := detector.DetectCapabilities(context.Background(), ports.ProviderCredentials{
		BaseURL:    "https://provider.example.test/v1",
		APIKey:     []byte("synthetic-key"),
		Model:      "synthetic-model",
		OutputMode: ports.ProviderOutputModeJSONObject,
		Version:    1,
	})
	if !result.Passed {
		t.Fatalf("JSON object detection failed: %s", result.SafeMessage)
	}
}

func TestDetectorClassifiesAuthenticationFailure(t *testing.T) {
	t.Parallel()

	detector := testDetector(t)
	detector.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    request,
		}, nil
	})}
	result := detector.DetectCapabilities(context.Background(), ports.ProviderCredentials{
		BaseURL:    "https://provider.example.test/v1",
		APIKey:     []byte("wrong"),
		Model:      "synthetic-model",
		OutputMode: ports.ProviderOutputModeJSONSchema,
	})
	if result.Passed || result.SafeMessage != "Provider 认证失败" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDetectorRejectsProviderTransportContractViolation(t *testing.T) {
	t.Parallel()

	detector := testDetector(t)
	detector.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		responseBody := []byte(`{
			"choices":[{"message":{"content":"{\"schema_version\":\"bill-visible-text/2\",\"document_type\":\"unknown\",\"payment\":null}"}}]
		}`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}
	result := detector.DetectCapabilities(context.Background(), ports.ProviderCredentials{
		BaseURL:    "https://provider.example.test/v1",
		APIKey:     []byte("synthetic-key"),
		Model:      "synthetic-model",
		OutputMode: ports.ProviderOutputModeJSONSchema,
	})
	if result.Passed || result.SafeMessage != "Provider 结构化输出不符合当前传输契约" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCapabilityProbeMeetsVisionInputMinimum(t *testing.T) {
	t.Parallel()

	encoded, err := blueProbeDataURL()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != capabilityProbeSide || decoded.Bounds().Dy() != capabilityProbeSide || capabilityProbeSide <= 10 {
		t.Fatalf("capability probe dimensions = %v", decoded.Bounds())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testDetector(t *testing.T) *Detector {
	t.Helper()
	schema := extractionSchemaPath(t)
	if _, err := os.Stat(schema); err != nil {
		t.Fatal(err)
	}
	detector, err := NewDetector(schema)
	if err != nil {
		t.Fatal(err)
	}
	return detector
}

func extractionSchemaPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../contracts/schemas/bill-visible-text.schema.json"))
}
