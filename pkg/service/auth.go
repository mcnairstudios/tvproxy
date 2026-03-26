package service

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

type TokenClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

type AuthService struct {
	userRepo      *repository.UserRepository
	jwtSecret     []byte
	accessExpiry  time.Duration
	refreshExpiry time.Duration
	inviteExpiry  time.Duration
}

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

func (s *AuthService) Login(ctx context.Context, username, password string) (accessToken, refreshToken string, err error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return "", "", fmt.Errorf("invalid credentials")
	}

	if user.PasswordHash == "" {
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

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (string, error) {
	claims, err := s.ValidateToken(refreshToken)
	if err != nil {
		return "", fmt.Errorf("invalid refresh token: %w", err)
	}

	subject, err := claims.GetSubject()
	if err != nil || subject != "refresh" {
		return "", fmt.Errorf("token is not a refresh token")
	}

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

func (s *AuthService) ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (any, error) {
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

func (s *AuthService) ListUsers(ctx context.Context) ([]models.User, error) {
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return users, nil
}

func (s *AuthService) GetUser(ctx context.Context, id string) (*models.User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	return user, nil
}

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

func (s *AuthService) DeleteUser(ctx context.Context, id string) error {
	if err := s.userRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return nil
}

func (s *AuthService) FindFirstAdmin(ctx context.Context) (*models.User, error) {
	return s.userRepo.GetFirstAdmin(ctx)
}

func (s *AuthService) SetInviteExpiry(d time.Duration) {
	s.inviteExpiry = d
}

func (s *AuthService) CreateInvite(ctx context.Context, username string) (*models.User, error) {
	token := uuid.New().String()
	expiry := 7 * 24 * time.Hour
	if s.inviteExpiry > 0 {
		expiry = s.inviteExpiry
	}
	expires := time.Now().Add(expiry)
	user := &models.User{
		Username:        username,
		PasswordHash:    "",
		IsAdmin:         false,
		InviteToken:     &token,
		InviteExpiresAt: &expires,
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating invite: %w", err)
	}
	return user, nil
}

func (s *AuthService) AcceptInvite(ctx context.Context, token, password string) error {
	user, err := s.userRepo.GetByInviteToken(ctx, token)
	if err != nil {
		return fmt.Errorf("invalid invite token")
	}
	if user.InviteExpiresAt != nil && time.Now().After(*user.InviteExpiresAt) {
		return fmt.Errorf("invite token expired")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	user.PasswordHash = string(hash)
	user.InviteToken = nil
	user.InviteExpiresAt = nil
	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("accepting invite: %w", err)
	}
	return nil
}

func (s *AuthService) AuthenticateBasicAuth(ctx context.Context, username, password string) (*models.User, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if user.PasswordHash == "" {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return user, nil
}

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
