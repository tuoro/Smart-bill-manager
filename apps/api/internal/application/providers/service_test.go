package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestProviderInputAndPermissionBoundaries(t *testing.T) {
	service := Service{}
	viewer := domain.TenantContext{TenantID: "tenant", UserID: "user", Role: domain.RoleViewer}
	if _, err := service.Create(context.Background(), CreateInput{Tenant: viewer}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer create error = %v", err)
	}
	if _, err := service.Detect(context.Background(), viewer, "config"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer detect error = %v", err)
	}
	if _, err := service.Activate(context.Background(), viewer, "config"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer activate error = %v", err)
	}
	if _, err := service.List(context.Background(), viewer); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer list error = %v", err)
	}

	owner := domain.TenantContext{TenantID: "tenant", UserID: "user", Role: domain.RoleOwner}
	for _, input := range []CreateInput{
		{Tenant: owner, BaseURL: "not-a-url", APIKey: []byte("key"), Model: "model", OutputMode: ports.ProviderOutputModeJSONSchema},
		{Tenant: owner, BaseURL: "https://provider.example/v1", APIKey: []byte("key"), Model: "", OutputMode: ports.ProviderOutputModeJSONSchema},
		{Tenant: owner, BaseURL: "https://provider.example/v1", APIKey: nil, Model: "model", OutputMode: ports.ProviderOutputModeJSONSchema},
		{Tenant: owner, BaseURL: "https://provider.example/v1", APIKey: []byte("key"), Model: "model", OutputMode: "automatic"},
	} {
		if _, err := service.Create(context.Background(), input); err == nil {
			t.Fatalf("invalid provider input accepted: %#v", input)
		}
	}
}

func TestActivationRejectsStaleProviderSchemaCapability(t *testing.T) {
	t.Parallel()

	current := ports.ProviderSchemaIdentity{
		Version: "bill-visible-text-provider/1",
		SHA256:  strings.Repeat("c", 64),
	}
	service := Service{
		repository: providerRepositoryStub{config: ports.ProviderConfig{
			ID: "config", TenantID: "tenant", CapabilityStatus: "passed",
			CapabilitySchemaVersion: "bill-extraction-provider/0",
			CapabilitySchemaSHA256:  strings.Repeat("b", 64),
		}},
		detector: identityDetectorStub{identity: current},
	}
	_, err := service.Activate(context.Background(), domain.TenantContext{
		TenantID: "tenant", UserID: "owner", Role: domain.RoleOwner,
	}, "config")
	var rule *domain.RuleError
	if !errors.As(err, &rule) || rule.Code != "provider_capability_required" {
		t.Fatalf("stale activation error = %#v", err)
	}
}

type providerRepositoryStub struct {
	config ports.ProviderConfig
}

func (stub providerRepositoryStub) ListProviderConfigs(context.Context, string) ([]ports.ProviderConfig, error) {
	return []ports.ProviderConfig{stub.config}, nil
}

func (stub providerRepositoryStub) GetProviderConfig(context.Context, string, string) (ports.ProviderConfig, error) {
	return stub.config, nil
}

type identityDetectorStub struct {
	identity ports.ProviderSchemaIdentity
}

func (stub identityDetectorStub) ProviderSchemaIdentity() ports.ProviderSchemaIdentity {
	return stub.identity
}

func (identityDetectorStub) DetectCapabilities(context.Context, ports.ProviderCredentials) ports.CapabilityResult {
	return ports.CapabilityResult{}
}

func TestNormalizeProviderBaseURL(t *testing.T) {
	valid, err := normalizeBaseURL(" https://provider.example/v1/ ")
	if err != nil || valid != "https://provider.example/v1" {
		t.Fatalf("normalized URL = %q, error=%v", valid, err)
	}
	for _, value := range []string{
		"ftp://provider.example/v1",
		"https://user:pass@provider.example/v1",
		"https://provider.example/v1?debug=true",
		"https://provider.example/v1#fragment",
		"/relative",
	} {
		if _, err := normalizeBaseURL(value); err == nil {
			t.Fatalf("unsafe URL accepted: %s", value)
		}
	}
	config := ports.ProviderConfig{EncryptedAPIKey: []byte("secret")}
	if got := publicConfig(config); len(got.EncryptedAPIKey) != 0 {
		t.Fatal("public provider config retained ciphertext")
	}
}
