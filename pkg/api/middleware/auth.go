package middleware

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const apiKeyContextKey contextKey = "api_key"

// RequireAPIKey extracts the Bearer token from the Authorization header and
// stores it in the request context. It does NOT verify the key — that is
// delegated to the individual handler (which has access to the registry).
// Returns 401 when no token is present.
func RequireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		if token == header || token == "" {
			http.Error(w, `{"error":"invalid Authorization format, expected: Bearer <key>"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), apiKeyContextKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func APIKeyFromContext(ctx context.Context) string {
	v, _ := ctx.Value(apiKeyContextKey).(string)
	return v
}
