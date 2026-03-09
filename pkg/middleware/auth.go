package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type contextKey string

const userContextKey contextKey = "user"

type ContextUser struct {
	UserID   int64
	Username string
	IsAdmin  bool
}

func UserFromContext(ctx context.Context) *ContextUser {
	u, _ := ctx.Value(userContextKey).(*ContextUser)
	return u
}

type AuthMiddleware struct {
	authService *service.AuthService
	apiKey      string
}

func NewAuthMiddleware(authService *service.AuthService, apiKey string) *AuthMiddleware {
	return &AuthMiddleware{authService: authService, apiKey: apiKey}
}

// Authenticate checks for JWT bearer token or API key
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check API key first
		if m.apiKey != "" {
			if key := r.Header.Get("X-API-Key"); key == m.apiKey {
				ctx := context.WithValue(r.Context(), userContextKey, &ContextUser{
					UserID:   0,
					Username: "api-key",
					IsAdmin:  true,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// Check Bearer token
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid authorization header"})
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		claims, err := m.authService.ValidateToken(token)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, &ContextUser{
			UserID:   claims.UserID,
			Username: claims.Username,
			IsAdmin:  claims.IsAdmin,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin checks that the authenticated user is an admin
func (m *AuthMiddleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
