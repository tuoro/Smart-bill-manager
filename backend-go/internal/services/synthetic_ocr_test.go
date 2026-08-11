package services

import (
	"strings"

	"smart-bill-manager/internal/devtools/regressionfixtures"
)

func syntheticOCRText(lines ...string) string {
	return strings.Join(append([]string{regressionfixtures.SyntheticMarker}, lines...), "\n")
}
