package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildInputLiteral_StringField(t *testing.T) {
	out := buildInputLiteral(map[string]any{"status": "active"})
	assert.Equal(t, `status: "active"`, out)
}

func TestBuildInputLiteral_IntField(t *testing.T) {
	out := buildInputLiteral(map[string]any{"currentTip": 42})
	assert.Equal(t, "currentTip: 42", out)
}

func TestBuildInputLiteral_Int64Field(t *testing.T) {
	out := buildInputLiteral(map[string]any{"sizeBytes": int64(1024)})
	assert.Equal(t, "sizeBytes: 1024", out)
}

func TestBuildInputLiteral_Float64Field(t *testing.T) {
	out := buildInputLiteral(map[string]any{"score": float64(0.95)})
	assert.Equal(t, "score: 0.95", out)
}

func TestBuildInputLiteral_BoolField(t *testing.T) {
	out := buildInputLiteral(map[string]any{"success": true})
	assert.Equal(t, "success: true", out)
}

func TestBuildInputLiteral_UnknownTypeSkipped(t *testing.T) {
	// Slices are not handled; the field should be omitted entirely.
	out := buildInputLiteral(map[string]any{"tags": []string{"a", "b"}})
	assert.Equal(t, "", out)
}

func TestBuildInputLiteral_EmptyMap(t *testing.T) {
	out := buildInputLiteral(map[string]any{})
	assert.Equal(t, "", out)
}

func TestBuildInputLiteral_MultipleFields(t *testing.T) {
	out := buildInputLiteral(map[string]any{"status": "active", "currentTip": 10})
	// Map iteration order is non-deterministic; just verify both parts are present.
	assert.True(t, strings.Contains(out, `status: "active"`))
	assert.True(t, strings.Contains(out, "currentTip: 10"))
}
