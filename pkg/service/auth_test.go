package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavinmcnair/tvproxy/pkg/store"
)

func newTestAuthService(t *testing.T) *AuthService {
	t.Helper()
	dir := t.TempDir()
	userStore := store.NewUserStore(filepath.Join(dir, "users.json"))
	authService := NewAuthService(userStore, "test-jwt-secret-key", 15*time.Minute, 7*24*time.Hour)
	return authService
}

func TestCreateUser(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	user, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)
	require.NotNil(t, user)

	assert.NotZero(t, user.ID)
	assert.Equal(t, "testuser", user.Username)
	assert.False(t, user.IsAdmin)
	assert.NotEmpty(t, user.PasswordHash)
	assert.NotEqual(t, "password123", user.PasswordHash, "password should be hashed, not stored in plaintext")
	assert.False(t, user.CreatedAt.IsZero())
	assert.False(t, user.UpdatedAt.IsZero())

	fetched, err := authService.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "testuser", fetched.Username)
}

func TestCreateUserAdmin(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	user, err := authService.CreateUser(ctx, "adminuser", "adminpass", true)
	require.NoError(t, err)
	require.NotNil(t, user)

	assert.True(t, user.IsAdmin)
}

func TestCreateUserDuplicateUsername(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	_, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	_, err = authService.CreateUser(ctx, "testuser", "otherpassword", false)
	assert.Error(t, err, "creating a user with a duplicate username should fail")
}

func TestLogin(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	_, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	accessToken, refreshToken, err := authService.Login(ctx, "testuser", "password123")
	require.NoError(t, err)
	assert.NotEmpty(t, accessToken)
	assert.NotEmpty(t, refreshToken)
	assert.NotEqual(t, accessToken, refreshToken, "access and refresh tokens should be different")
}

func TestLoginWrongPassword(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	_, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	accessToken, refreshToken, err := authService.Login(ctx, "testuser", "wrongpassword")
	assert.Error(t, err)
	assert.Empty(t, accessToken)
	assert.Empty(t, refreshToken)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestLoginNonExistentUser(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	accessToken, refreshToken, err := authService.Login(ctx, "noone", "password123")
	assert.Error(t, err)
	assert.Empty(t, accessToken)
	assert.Empty(t, refreshToken)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestValidateToken(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	user, err := authService.CreateUser(ctx, "testuser", "password123", true)
	require.NoError(t, err)

	accessToken, _, err := authService.Login(ctx, "testuser", "password123")
	require.NoError(t, err)

	claims, err := authService.ValidateToken(accessToken)
	require.NoError(t, err)
	require.NotNil(t, claims)

	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, "testuser", claims.Username)
	assert.True(t, claims.IsAdmin)

	subject, err := claims.GetSubject()
	require.NoError(t, err)
	assert.Equal(t, "access", subject)

	expiresAt, err := claims.GetExpirationTime()
	require.NoError(t, err)
	assert.True(t, expiresAt.After(time.Now()), "token should not be expired yet")
}

func TestValidateTokenInvalid(t *testing.T) {
	authService := newTestAuthService(t)

	claims, err := authService.ValidateToken("this.is.not.a.valid.token")
	assert.Error(t, err)
	assert.Nil(t, claims)
}

func TestValidateTokenWrongSecret(t *testing.T) {
	dir := t.TempDir()
	userStore := store.NewUserStore(filepath.Join(dir, "users.json"))
	ctx := context.Background()

	authService1 := NewAuthService(userStore, "secret-one", 15*time.Minute, 7*24*time.Hour)
	_, err := authService1.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)
	accessToken, _, err := authService1.Login(ctx, "testuser", "password123")
	require.NoError(t, err)

	authService2 := NewAuthService(userStore, "secret-two", 15*time.Minute, 7*24*time.Hour)
	claims, err := authService2.ValidateToken(accessToken)
	assert.Error(t, err)
	assert.Nil(t, claims)
}

func TestRefreshToken(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	_, err := authService.CreateUser(ctx, "testuser", "password123", true)
	require.NoError(t, err)

	_, refreshToken, err := authService.Login(ctx, "testuser", "password123")
	require.NoError(t, err)

	newAccessToken, err := authService.RefreshToken(ctx, refreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, newAccessToken)

	claims, err := authService.ValidateToken(newAccessToken)
	require.NoError(t, err)
	assert.Equal(t, "testuser", claims.Username)
	assert.True(t, claims.IsAdmin)

	subject, err := claims.GetSubject()
	require.NoError(t, err)
	assert.Equal(t, "access", subject, "refreshed token should be an access token")
}

func TestRefreshTokenWithAccessTokenFails(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	_, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	accessToken, _, err := authService.Login(ctx, "testuser", "password123")
	require.NoError(t, err)

	_, err = authService.RefreshToken(ctx, accessToken)
	assert.Error(t, err, "should not be able to use an access token to refresh")
	assert.Contains(t, err.Error(), "not a refresh token")
}

func TestRefreshTokenInvalid(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	_, err := authService.RefreshToken(ctx, "invalid-token")
	assert.Error(t, err)
}

func TestListUsers(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	_, err := authService.CreateUser(ctx, "user1", "pass1", false)
	require.NoError(t, err)
	_, err = authService.CreateUser(ctx, "user2", "pass2", true)
	require.NoError(t, err)

	users, err := authService.ListUsers(ctx)
	require.NoError(t, err)
	assert.Len(t, users, 2)
	assert.Equal(t, "user1", users[0].Username)
	assert.Equal(t, "user2", users[1].Username)
}

func TestGetUser(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	created, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	fetched, err := authService.GetUser(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, "testuser", fetched.Username)
}

func TestUpdateUser(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	user, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	user.Username = "updateduser"
	user.IsAdmin = true
	err = authService.UpdateUser(ctx, user, "newpassword")
	require.NoError(t, err)

	fetched, err := authService.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "updateduser", fetched.Username)
	assert.True(t, fetched.IsAdmin)

	_, _, err = authService.Login(ctx, "updateduser", "newpassword")
	require.NoError(t, err)

	_, _, err = authService.Login(ctx, "updateduser", "password123")
	assert.Error(t, err)
}

func TestUpdateUserWithoutPasswordChange(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	user, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	user.IsAdmin = true
	err = authService.UpdateUser(ctx, user, "")
	require.NoError(t, err)

	_, _, err = authService.Login(ctx, "testuser", "password123")
	require.NoError(t, err)
}

func TestDeleteUser(t *testing.T) {
	authService := newTestAuthService(t)
	ctx := context.Background()

	user, err := authService.CreateUser(ctx, "testuser", "password123", false)
	require.NoError(t, err)

	err = authService.DeleteUser(ctx, user.ID)
	require.NoError(t, err)

	_, err = authService.GetUser(ctx, user.ID)
	assert.Error(t, err, "user should no longer exist after deletion")
}
