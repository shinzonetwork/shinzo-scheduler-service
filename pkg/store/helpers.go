package store

import (
	"fmt"
	"strings"
)

// buildInputLiteral converts a map of field name → value into a GraphQL input literal
// suitable for inline use inside mutation input: {...} blocks.
// Only string, int, float64, and bool value types are handled — all others are skipped.
func buildInputLiteral(fields map[string]any) string {
	parts := make([]string, 0, len(fields))
	for k, v := range fields {
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s: %q", k, val))
		case int:
			parts = append(parts, fmt.Sprintf("%s: %d", k, val))
		case int64:
			parts = append(parts, fmt.Sprintf("%s: %d", k, val))
		case float64:
			parts = append(parts, fmt.Sprintf("%s: %g", k, val))
		case bool:
			parts = append(parts, fmt.Sprintf("%s: %t", k, val))
		}
	}
	return strings.Join(parts, ", ")
}
