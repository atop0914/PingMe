package password

import (
	"testing"
	"time"

	"PingMe/internal/config"
	"PingMe/internal/pkg/jwt"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPasswordHashing tests password hashing and comparison
func TestPasswordHashing(t *testing.T) {
	testPassword := "testPassword123"
	
	// Test hashing
	hash, err := Hash(testPassword)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, testPassword, hash)

	// Test correct password comparison
	isValid := Compare(testPassword, hash)
	assert.True(t, isValid)

	// Test wrong password comparison
	isValid = Compare("wrongPassword", hash)
	assert.False(t, isValid)
}

// TestPasswordHashing_Uniqueness tests that same password generates different hashes
func TestPasswordHashing_Uniqueness(t *testing.T) {
	testPassword := "testPassword123"
	
	hash1, err := Hash(testPassword)
	require.NoError(t, err)
	
	hash2, err := Hash(testPassword)
	require.NoError(t, err)
	
	// Hashes should be different due to salt
	assert.NotEqual(t, hash1, hash2)
	
	// But both should validate correctly
	assert.True(t, Compare(testPassword, hash1))
	assert.True(t, Compare(testPassword, hash2))
}

// TestJWTTokenGeneration tests JWT token generation
func TestJWTTokenGeneration(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:     "test-secret-key",
		Expiration: 86400,
	}
	
	jwtSvc := jwt.NewTokenService(cfg)
	
	userID := uuid.New().String()
	username := "testuser"
	
	// Generate token
	token, err := jwtSvc.GenerateToken(userID, username)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	
	// Validate token
	claims, err := jwtSvc.ValidateToken(token)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, username, claims.Username)
	assert.Equal(t, "PingMe", claims.Issuer)
}

// TestJWTTokenValidation_InvalidToken tests invalid token validation
func TestJWTTokenValidation_InvalidToken(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:     "test-secret-key",
		Expiration: 86400,
	}
	
	jwtSvc := jwt.NewTokenService(cfg)
	
	// Test with invalid token
	_, err := jwtSvc.ValidateToken("invalid-token")
	assert.Error(t, err)
}

// TestJWTTokenValidation_WrongSecret tests token validation with wrong secret
func TestJWTTokenValidation_WrongSecret(t *testing.T) {
	cfg1 := &config.JWTConfig{
		Secret:     "secret1",
		Expiration: 86400,
	}
	
	cfg2 := &config.JWTConfig{
		Secret:     "secret2",
		Expiration: 86400,
	}
	
	jwtSvc1 := jwt.NewTokenService(cfg1)
	jwtSvc2 := jwt.NewTokenService(cfg2)
	
	userID := uuid.New().String()
	username := "testuser"
	
	// Generate token with secret1
	token, err := jwtSvc1.GenerateToken(userID, username)
	require.NoError(t, err)
	
	// Validate with secret2 should fail
	_, err = jwtSvc2.ValidateToken(token)
	assert.Error(t, err)
}

// TestJWTTokenExpirationDuration tests token expiration duration
func TestJWTTokenExpirationDuration(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:     "test-secret-key",
		Expiration: 3600, // 1 hour
	}
	
	jwtSvc := jwt.NewTokenService(cfg)
	
	assert.Equal(t, time.Hour, jwtSvc.GetExpirationDuration())
}
