package jwt

import (
	"errors"
	"time"

	"PingMe/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// TokenService handles JWT token operations
type TokenService struct {
	secret     []byte
	expiration time.Duration
}

// NewTokenService creates a new token service
func NewTokenService(cfg *config.JWTConfig) *TokenService {
	return &TokenService{
		secret:     []byte(cfg.Secret),
		expiration: time.Duration(cfg.Expiration) * time.Second,
	}
}

// GenerateToken generates a new JWT token for a user
func (s *TokenService) GenerateToken(userID, username string) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "PingMe",
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ValidateToken validates a JWT token and returns the claims
func (s *TokenService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// GetExpirationDuration returns the token expiration duration
func (s *TokenService) GetExpirationDuration() time.Duration {
	return s.expiration
}
