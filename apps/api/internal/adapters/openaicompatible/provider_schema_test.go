package openaicompatible

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestProviderSchemaProjectionIsDeterministicStrictRoot(t *testing.T) {
	t.Parallel()

	detector := testDetector(t)
	second := testDetector(t)
	identity := detector.ProviderSchemaIdentity()
	if identity.Version != "bill-visible-text-provider/1" || len(identity.SHA256) != 64 {
		t.Fatalf("provider schema identity = %#v", identity)
	}
	if identity != second.ProviderSchemaIdentity() {
		t.Fatalf("provider schema projection is not deterministic: %#v / %#v", identity, second.ProviderSchemaIdentity())
	}
	assertProviderSchemaNode(t, detector.providerSchema, "$")

	encoded, err := json.Marshal(detector.providerSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		`"uniqueItems"`, `"allOf"`, `"if"`, `"then"`, `"not"`,
		`"minimum"`, `"maximum"`, `"minLength"`, `"maxLength"`, `"pattern"`,
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("provider schema retained local-only keyword %s", forbidden)
		}
	}

	canonical, err := os.ReadFile(extractionSchemaPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canonical), `"additionalProperties": false`) ||
		!strings.Contains(string(canonical), `"const": "bill-visible-text/1"`) {
		t.Fatal("authoritative schema lost the closed minimal root identity boundary")
	}

	root := detector.providerSchema.(map[string]any)
	if root["additionalProperties"] != false {
		t.Fatalf("Provider root must be closed for strict structured output: %#v", root)
	}
	properties := root["properties"].(map[string]any)
	for _, name := range []string{"schema_version", "document_type", "payment", "invoice"} {
		if _, exists := properties[name]; !exists {
			t.Fatalf("Provider schema is missing declared root member %s", name)
		}
	}
	if strings.Contains(string(encoded), "amount_minor") || strings.Contains(string(encoded), "value_minor") ||
		strings.Contains(string(encoded), `"evidence"`) {
		t.Fatal("Provider schema exposes an internal or retired wrapper shape")
	}
}

func TestProviderSchemaProjectionClosesOpenCanonicalObjectDeterministically(t *testing.T) {
	t.Parallel()
	projected, _, err := projectProviderSchema(map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"payload": map[string]any{}},
		"required":             []any{},
		"additionalProperties": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := projected.(map[string]any)
	if root["additionalProperties"] != false {
		t.Fatalf("open canonical object was not closed in Provider projection: %#v", root)
	}
}

func TestProviderSchemaProjectionRejectsUnclassifiedKeywords(t *testing.T) {
	t.Parallel()

	_, _, err := projectProviderSchema(map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []any{},
		"additionalProperties": false,
		"futureConstraint":     true,
	})
	if err == nil || !strings.Contains(err.Error(), "unclassified JSON Schema keyword") {
		t.Fatalf("unclassified keyword error = %v", err)
	}
}

func assertProviderSchemaNode(t *testing.T, node any, path string) {
	t.Helper()
	object, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want schema object", path, node)
	}
	if rawProperties, found := object["properties"]; found {
		properties := rawProperties.(map[string]any)
		requiredRaw := object["required"].([]string)
		required := append([]string(nil), requiredRaw...)
		propertyNames := make([]string, 0, len(properties))
		for name, child := range properties {
			propertyNames = append(propertyNames, name)
			assertProviderSchemaNode(t, child, path+".properties."+name)
		}
		sort.Strings(propertyNames)
		if !reflect.DeepEqual(required, propertyNames) {
			t.Fatalf("%s required = %#v, properties = %#v", path, required, propertyNames)
		}
		if object["additionalProperties"] != false {
			t.Fatalf("%s is not a closed object", path)
		}
	}
	if definitions, found := object["$defs"]; found {
		for name, child := range definitions.(map[string]any) {
			assertProviderSchemaNode(t, child, path+".$defs."+name)
		}
	}
	if items, found := object["items"]; found {
		assertProviderSchemaNode(t, items, path+".items")
	}
	if alternatives, found := object["anyOf"]; found {
		items := alternatives.([]any)
		if len(items) != 2 || items[1].(map[string]any)["type"] != "null" {
			t.Fatalf("%s contains a non-nullability anyOf: %#v", path, items)
		}
		for index, child := range items {
			assertProviderSchemaNode(t, child, path+".anyOf["+string(rune('0'+index))+"]")
		}
	}
}

func containsJSONValue(values []any, expected any) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
