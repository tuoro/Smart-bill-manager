package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAHandlerRoutesAndCachePolicy(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>M1</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app-123.js"), []byte("export {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newSPAHandler(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, route := range []string{"/", "/inbox", "/reviews/job-id"} {
		request := httptest.NewRequest(http.MethodGet, route, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "M1") {
			t.Fatalf("route %s did not serve index: status=%d body=%q", route, response.Code, response.Body.String())
		}
		if cache := response.Header().Get("Cache-Control"); cache != "no-store" {
			t.Fatalf("route %s cache policy = %q", route, cache)
		}
	}

	assetRequest := httptest.NewRequest(http.MethodGet, "/assets/app-123.js", nil)
	assetResponse := httptest.NewRecorder()
	handler.ServeHTTP(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusOK || assetResponse.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset response status=%d cache=%q", assetResponse.Code, assetResponse.Header().Get("Cache-Control"))
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d", missingResponse.Code)
	}
}

func TestSPAHandlerRequiresIndex(t *testing.T) {
	if _, err := newSPAHandler(t.TempDir()); err == nil {
		t.Fatal("expected missing index error")
	}
}

func TestSecurityHeadersSeparateAPIAndApplication(t *testing.T) {
	server := &Server{logger: slog.Default(), config: Config{CookieSecure: true}}
	handler := server.securityHeaders(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))

	apiRequest := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, apiRequest)
	if policy := apiResponse.Header().Get("Content-Security-Policy"); policy != "default-src 'none'; frame-ancestors 'none'" {
		t.Fatalf("API CSP = %q", policy)
	}
	if apiResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("API response must not be cached")
	}

	appRequest := httptest.NewRequest(http.MethodGet, "/inbox", nil)
	appResponse := httptest.NewRecorder()
	handler.ServeHTTP(appResponse, appRequest)
	policy := appResponse.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'self'") || !strings.Contains(policy, "frame-src 'self'") {
		t.Fatalf("application CSP = %q", policy)
	}
	if appResponse.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("secure deployments must send HSTS")
	}
}
