package openaicompatible

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const providerSchemaVersion = "bill-visible-text-provider/1"

var providerSchemaKeywords = map[string]struct{}{
	"$defs":                {},
	"$ref":                 {},
	"additionalProperties": {},
	"const":                {},
	"enum":                 {},
	"items":                {},
	"properties":           {},
	"required":             {},
	"type":                 {},
}

var localValidationKeywords = map[string]struct{}{
	"$id":               {},
	"$schema":           {},
	"allOf":             {},
	"anyOf":             {},
	"default":           {},
	"dependentRequired": {},
	"description":       {},
	"else":              {},
	"exclusiveMaximum":  {},
	"exclusiveMinimum":  {},
	"examples":          {},
	"format":            {},
	"if":                {},
	"maxItems":          {},
	"maxLength":         {},
	"maximum":           {},
	"minItems":          {},
	"minLength":         {},
	"minimum":           {},
	"multipleOf":        {},
	"not":               {},
	"oneOf":             {},
	"pattern":           {},
	"then":              {},
	"title":             {},
	"uniqueItems":       {},
}

func projectProviderSchema(canonical any) (any, ports.ProviderSchemaIdentity, error) {
	if err := validateSchemaKeywords(canonical, "$", true); err != nil {
		return nil, ports.ProviderSchemaIdentity{}, err
	}
	projected, err := projectSchemaNode(canonical, "$")
	if err != nil {
		return nil, ports.ProviderSchemaIdentity{}, err
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return nil, ports.ProviderSchemaIdentity{}, fmt.Errorf("encode provider schema: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return projected, ports.ProviderSchemaIdentity{
		Version: providerSchemaVersion,
		SHA256:  hex.EncodeToString(digest[:]),
	}, nil
}

func validateSchemaKeywords(node any, path string, schemaNode bool) error {
	object, ok := node.(map[string]any)
	if !ok {
		if _, booleanSchema := node.(bool); booleanSchema {
			return fmt.Errorf("provider schema projection does not support boolean schema at %s", path)
		}
		return nil
	}
	if schemaNode {
		for keyword := range object {
			if _, preserved := providerSchemaKeywords[keyword]; preserved {
				continue
			}
			if _, localOnly := localValidationKeywords[keyword]; localOnly {
				continue
			}
			return fmt.Errorf("unclassified JSON Schema keyword %q at %s", keyword, path)
		}
	}
	for keyword, value := range object {
		switch keyword {
		case "$defs", "properties":
			children, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.%s must be an object", path, keyword)
			}
			for name, child := range children {
				if err := validateSchemaKeywords(child, path+"."+keyword+"."+name, true); err != nil {
					return err
				}
			}
		case "items", "additionalProperties", "not", "if", "then", "else":
			if objectValue, isObject := value.(map[string]any); isObject {
				if err := validateSchemaKeywords(objectValue, path+"."+keyword, true); err != nil {
					return err
				}
			}
		case "allOf", "anyOf", "oneOf":
			children, ok := value.([]any)
			if !ok {
				return fmt.Errorf("%s.%s must be an array", path, keyword)
			}
			for index, child := range children {
				if err := validateSchemaKeywords(child, fmt.Sprintf("%s.%s[%d]", path, keyword, index), true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func projectSchemaNode(node any, path string) (map[string]any, error) {
	source, ok := node.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema at %s must be an object", path)
	}
	result := make(map[string]any)
	for _, keyword := range []string{"$ref", "type", "enum", "const"} {
		if value, found := source[keyword]; found {
			result[keyword] = cloneJSONValue(value)
		}
	}
	if rawDefinitions, found := source["$defs"]; found {
		definitions, ok := rawDefinitions.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.$defs must be an object", path)
		}
		projectedDefinitions := make(map[string]any, len(definitions))
		for name, definition := range definitions {
			projected, err := projectSchemaNode(definition, path+".$defs."+name)
			if err != nil {
				return nil, err
			}
			projectedDefinitions[name] = projected
		}
		result["$defs"] = projectedDefinitions
	}
	if rawItems, found := source["items"]; found {
		projected, err := projectSchemaNode(rawItems, path+".items")
		if err != nil {
			return nil, err
		}
		result["items"] = projected
	}
	rawProperties, hasProperties := source["properties"]
	if hasProperties {
		properties, ok := rawProperties.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.properties must be an object", path)
		}
		originalRequired, err := requiredSet(source["required"], path)
		if err != nil {
			return nil, err
		}
		projectedProperties := make(map[string]any, len(properties))
		required := make([]string, 0, len(properties))
		for name, property := range properties {
			propertyPath := path + ".properties." + name
			projected, err := projectSchemaNode(property, propertyPath)
			if err != nil {
				return nil, err
			}
			if _, isRequired := originalRequired[name]; !isRequired {
				projected = nullableSchema(projected)
			}
			projectedProperties[name] = projected
			required = append(required, name)
		}
		sort.Strings(required)
		result["properties"] = projectedProperties
		result["required"] = required
	}
	if isObjectSchema(source, hasProperties) {
		additional, found := source["additionalProperties"]
		if found {
			if _, boolean := additional.(bool); !boolean {
				return nil, fmt.Errorf("object schema at %s must use a boolean additionalProperties value", path)
			}
		}
		// Strict structured-output providers require closed objects. Projection
		// closes the declared minimal visible-text shape deterministically without
		// adding a Provider-specific branch.
		result["additionalProperties"] = false
		if !hasProperties {
			result["properties"] = map[string]any{}
			result["required"] = []string{}
		}
	}
	return result, nil
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, child := range typed {
			cloned[key] = cloneJSONValue(child)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index := range typed {
			cloned[index] = cloneJSONValue(typed[index])
		}
		return cloned
	default:
		return value
	}
}

func requiredSet(value any, path string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if value == nil {
		return result, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s.required must be an array", path)
	}
	for _, item := range items {
		name, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s.required must contain strings", path)
		}
		result[name] = struct{}{}
	}
	return result, nil
}

func isObjectSchema(source map[string]any, hasProperties bool) bool {
	if hasProperties {
		return true
	}
	switch value := source["type"].(type) {
	case string:
		return value == "object"
	case []any:
		for _, item := range value {
			if item == "object" {
				return true
			}
		}
	}
	return false
}

func nullableSchema(schema map[string]any) map[string]any {
	if rawType, found := schema["type"]; found {
		switch value := rawType.(type) {
		case string:
			if value != "null" {
				schema["type"] = []any{value, "null"}
			}
			return schema
		case []any:
			for _, item := range value {
				if item == "null" {
					return schema
				}
			}
			schema["type"] = append(value, "null")
			return schema
		}
	}
	return map[string]any{
		"anyOf": []any{schema, map[string]any{"type": "null"}},
	}
}

func verifyProviderSchemaIdentity(actual, expected ports.ProviderSchemaIdentity) error {
	if actual.Version == "" || actual.SHA256 == "" {
		return errors.New("provider schema identity is missing")
	}
	if actual != expected {
		return errors.New("provider schema identity is stale")
	}
	return nil
}
