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
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

func UserFromContext(ctx context.Context) *ContextUser {
	u, _ := ctx.Value(userContextKey).(*ContextUser)
	return u
}

type AuthMiddleware struct {
	authService     *service.AuthService
	activityService *service.ActivityService
	apiKey          string
	adminUserID     string
}

func NewAuthMiddleware(authService *service.AuthService, activityService *service.ActivityService, apiKey string, adminUserID string) *AuthMiddleware {
	return &AuthMiddleware{authService: authService, activityService: activityService, apiKey: apiKey, adminUserID: adminUserID}
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.apiKey != "" {
			if key := r.Header.Get("X-API-Key"); key == m.apiKey {
				ctx := context.WithValue(r.Context(), userContextKey, &ContextUser{
					UserID:   m.adminUserID,
					Username: "api-key",
					IsAdmin:  true,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		var token string
		if auth := r.Header.Get("Authorization"); auth != "" && strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if qToken := r.URL.Query().Get("token"); qToken != "" {
			token = qToken
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing or invalid authorization header"})
			return
		}
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
		if m.activityService != nil {
			m.activityService.TouchUser(claims.UserID, claims.Username, "Dashboard", r.RemoteAddr, r.UserAgent())
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

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
