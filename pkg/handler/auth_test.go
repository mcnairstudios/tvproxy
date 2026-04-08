package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type testEnv struct {
	router      *chi.Mux
	authService *service.AuthService
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	userStore := store.NewUserStore(filepath.Join(dir, "users.json"))
	authService := service.NewAuthService(userStore, "test-jwt-secret", 15*time.Minute, 7*24*time.Hour)

	_, err := authService.CreateUser(context.Background(), "testuser", "password123", false)
	require.NoError(t, err)

	_, err = authService.CreateUser(context.Background(), "admin", "adminpass", true)
	require.NoError(t, err)

	authHandler := NewAuthHandler(authService)
	authMW := middleware.NewAuthMiddleware(authService, nil, "", "")

	r := chi.NewRouter()

	r.Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/refresh", authHandler.Refresh)

	r.Group(func(r chi.Router) {
		r.Use(authMW.Authenticate)
		r.Get("/api/auth/me", authHandler.Me)
		r.Post("/api/auth/logout", authHandler.Logout)
	})

	return &testEnv{
		router:      r,
		authService: authService,
	}
}

func loginTestUser(t *testing.T, env *testEnv, username, password string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	return resp["access_token"], resp["refresh_token"]
}

func TestLoginHandler(t *testing.T) {
	env := setupTestEnv(t)

	body, _ := json.Marshal(map[string]string{
		"username": "testuser",
		"password": "password123",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp["access_token"])
	assert.NotEmpty(t, resp["refresh_token"])
}

func TestLoginHandlerBadCredentials(t *testing.T) {
	env := setupTestEnv(t)

	body, _ := json.Marshal(map[string]string{
		"username": "testuser",
		"password": "wrongpassword",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "invalid credentials")
}

func TestLoginHandlerMissingFields(t *testing.T) {
	env := setupTestEnv(t)

	body, _ := json.Marshal(map[string]string{
		"username": "testuser",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	body, _ = json.Marshal(map[string]string{
		"password": "password123",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginHandlerInvalidJSON(t *testing.T) {
	env := setupTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginHandlerNonExistentUser(t *testing.T) {
	env := setupTestEnv(t)

	body, _ := json.Marshal(map[string]string{
		"username": "noone",
		"password": "password123",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMeHandler(t *testing.T) {
	env := setupTestEnv(t)

	accessToken, _ := loginTestUser(t, env, "testuser", "password123")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "testuser", resp["username"])
	assert.Equal(t, false, resp["is_admin"])
}

func TestMeHandlerAdmin(t *testing.T) {
	env := setupTestEnv(t)

	accessToken, _ := loginTestUser(t, env, "admin", "adminpass")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "admin", resp["username"])
	assert.Equal(t, true, resp["is_admin"])
}

func TestMeHandlerNoAuth(t *testing.T) {
	env := setupTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMeHandlerInvalidToken(t *testing.T) {
	env := setupTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-here")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRefreshHandler(t *testing.T) {
	env := setupTestEnv(t)

	_, refreshToken := loginTestUser(t, env, "testuser", "password123")

	body, _ := json.Marshal(map[string]string{
		"refresh_token": refreshToken,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["access_token"])
}

func TestRefreshHandlerInvalidToken(t *testing.T) {
	env := setupTestEnv(t)

	body, _ := json.Marshal(map[string]string{
		"refresh_token": "invalid-refresh-token",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRefreshHandlerMissingToken(t *testing.T) {
	env := setupTestEnv(t)

	body, _ := json.Marshal(map[string]string{})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLogoutHandler(t *testing.T) {
	env := setupTestEnv(t)

	accessToken, _ := loginTestUser(t, env, "testuser", "password123")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "logged out", resp["message"])
}

func TestAPIKeyAuthentication(t *testing.T) {
	dir := t.TempDir()
	userStore := store.NewUserStore(filepath.Join(dir, "users.json"))
	authService := service.NewAuthService(userStore, "test-jwt-secret", 15*time.Minute, 7*24*time.Hour)

	authHandler := NewAuthHandler(authService)
	authMW := middleware.NewAuthMiddleware(authService, nil, "my-api-key", "")

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(authMW.Authenticate)
		r.Get("/api/auth/me", authHandler.Me)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("X-API-Key", "my-api-key")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "api-key", resp["username"])
	assert.Equal(t, true, resp["is_admin"])
}

func TestAPIKeyAuthenticationWrongKey(t *testing.T) {
	dir := t.TempDir()
	userStore := store.NewUserStore(filepath.Join(dir, "users.json"))
	authService := service.NewAuthService(userStore, "test-jwt-secret", 15*time.Minute, 7*24*time.Hour)

	authHandler := NewAuthHandler(authService)
	authMW := middleware.NewAuthMiddleware(authService, nil, "my-api-key", "")

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(authMW.Authenticate)
		r.Get("/api/auth/me", authHandler.Me)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("X-API-Key", "wrong-api-key")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
