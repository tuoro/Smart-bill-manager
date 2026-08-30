package openaicompatible

import (
	"encoding/json"
	"errors"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const jsonObjectSchemaInstruction = " The response must be an instance of this Provider-facing JSON Schema; do not return or explain the schema itself: "

func outputContract(mode string, providerSchema any, includeJSONObjectSchema bool) (map[string]any, string, error) {
	switch mode {
	case ports.ProviderOutputModeJSONSchema:
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "bill_visible_text_provider",
				"strict": true,
				"schema": providerSchema,
			},
		}, "", nil
	case ports.ProviderOutputModeJSONObject:
		if !includeJSONObjectSchema {
			// Visible-text extraction has its own exact task contract. JSON Object mode only
			// guarantees JSON syntax; the projected Provider schema remains the
			// mandatory local validation gate without duplicating it in the prompt.
			return map[string]any{"type": "json_object"}, "", nil
		}
		encoded, err := json.Marshal(providerSchema)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{"type": "json_object"}, jsonObjectSchemaInstruction + string(encoded), nil
	default:
		return nil, "", errors.New("unsupported Provider output mode")
	}
}
