package service

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// TokenClaims holds the custom claims embedded in JWT tokens.
type TokenClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// AuthService handles authentication, user management, and JWT token operations.
type AuthService struct {
	userRepo       *repository.UserRepository
	jwtSecret      []byte
	accessExpiry   time.Duration
	refreshExpiry  time.Duration
}

// NewAuthService creates a new AuthService.
func NewAuthService(
	userRepo *repository.UserRepository,
	jwtSecret string,
	accessExpiry time.Duration,
	refreshExpiry time.Duration,
) *AuthService {
	return &AuthService{
		userRepo:      userRepo,
		jwtSecret:     []byte(jwtSecret),
		accessExpiry:  accessExpiry,
		refreshExpiry: refreshExpiry,
	}
}

// Login authenticates a user with username and password, returning access and refresh tokens.
func (s *AuthService) Login(ctx context.Context, username, password string) (accessToken, refreshToken string, err error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return "", "", fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", "", fmt.Errorf("invalid credentials")
	}

	accessToken, err = s.generateToken(user, s.accessExpiry, "access")
	if err != nil {
		return "", "", fmt.Errorf("generating access token: %w", err)
	}

	refreshToken, err = s.generateToken(user, s.refreshExpiry, "refresh")
	if err != nil {
		return "", "", fmt.Errorf("generating refresh token: %w", err)
	}

	return accessToken, refreshToken, nil
}

// RefreshToken validates a refresh token and issues a new access token.
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (string, error) {
	claims, err := s.ValidateToken(refreshToken)
	if err != nil {
		return "", fmt.Errorf("invalid refresh token: %w", err)
	}

	subject, err := claims.GetSubject()
	if err != nil || subject != "refresh" {
		return "", fmt.Errorf("token is not a refresh token")
	}

	// Verify the user still exists
	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		return "", fmt.Errorf("user no longer exists: %w", err)
	}

	accessToken, err := s.generateToken(user, s.accessExpiry, "access")
	if err != nil {
		return "", fmt.Errorf("generating access token: %w", err)
	}

	return accessToken, nil
}

// ValidateToken parses and validates a JWT token string, returning the claims.
func (s *AuthService) ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// CreateUser creates a new user with a hashed password.
func (s *AuthService) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	user := &models.User{
		Username:     username,
		PasswordHash: string(hash),
		IsAdmin:      isAdmin,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

// ListUsers returns all users.
func (s *AuthService) ListUsers(ctx context.Context) ([]models.User, error) {
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return users, nil
}

// GetUser returns a user by ID.
func (s *AuthService) GetUser(ctx context.Context, id int64) (*models.User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	return user, nil
}

// UpdateUser updates a user. If newPassword is non-empty, the password is re-hashed.
func (s *AuthService) UpdateUser(ctx context.Context, user *models.User, newPassword string) error {
	if newPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hashing password: %w", err)
		}
		user.PasswordHash = string(hash)
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

// DeleteUser deletes a user by ID.
func (s *AuthService) DeleteUser(ctx context.Context, id int64) error {
	if err := s.userRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return nil
}

// generateToken creates a signed JWT token for the given user.
func (s *AuthService) generateToken(user *models.User, expiry time.Duration, subject string) (string, error) {
	now := time.Now()
	claims := &TokenClaims{
		UserID:   user.ID,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}

	return signed, nil
}
