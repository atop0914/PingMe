package middleware

import (
	"net/http"
	"strings"

	"PingMe/internal/errorcode"
	"PingMe/internal/pkg/jwt"
	"PingMe/pkg/response"

	"github.com/gin-gonic/gin"
)

// ContextKey for user claims
type ContextKey string

const (
	// UserClaimsKey is the context key for user claims
	UserClaimsKey ContextKey = "user_claims"
)

// AuthMiddleware creates a JWT authentication middleware
func AuthMiddleware(jwtSvc *jwt.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Fail(errorcode.InvalidToken))
			return
		}

		// Check Bearer prefix
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Fail(errorcode.InvalidToken))
			return
		}

		tokenString := parts[1]

		// Validate token
		claims, err := jwtSvc.ValidateToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Fail(errorcode.TokenExpired))
			return
		}

		// Set user claims in context
		c.Set(string(UserClaimsKey), claims)

		c.Next()
	}
}

// GetUserClaims retrieves user claims from context
func GetUserClaims(c *gin.Context) *jwt.Claims {
	if claims, exists := c.Get(string(UserClaimsKey)); exists {
		if userClaims, ok := claims.(*jwt.Claims); ok {
			return userClaims
		}
	}
	return nil
}
