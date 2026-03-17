package middleware

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/gamewinner2019/FlowManPay/internal/config"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
)

// TokenType distinguishes access and refresh tokens
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

// Claims represents JWT claims
type Claims struct {
	UserID uint      `json:"user_id"`
	Type   TokenType `json:"type"`
	jwt.RegisteredClaims
}

// GenerateAccessToken creates an access token for the given user ID
func GenerateAccessToken(userID uint) (string, error) {
	cfg := config.Get()
	claims := Claims{
		UserID: userID,
		Type:   AccessToken,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(cfg.JWT.AccessExpireMinutes) * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWT.Secret))
}

// GenerateRefreshToken creates a refresh token for the given user ID
func GenerateRefreshToken(userID uint) (string, error) {
	cfg := config.Get()
	claims := Claims{
		UserID: userID,
		Type:   RefreshToken,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(cfg.JWT.RefreshExpireMinutes) * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWT.Secret))
}

// ParseToken parses and validates a JWT token string
func ParseToken(tokenString string) (*Claims, error) {
	cfg := config.Get()
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("无效的签名方法")
		}
		return []byte(cfg.JWT.Secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("无效的Token")
	}
	return claims, nil
}

// JWTAuth is the JWT authentication middleware.
// It extracts the token from the Authorization header, validates it,
// and sets the user_id in the context.
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.ErrorResponse(c, "未提供认证信息", 4001)
			c.Abort()
			return
		}

		// Support "Bearer <token>" and "JWT <token>" formats
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || (parts[0] != "Bearer" && parts[0] != "JWT") {
			response.ErrorResponse(c, "认证格式错误", 4001)
			c.Abort()
			return
		}

		claims, err := ParseToken(parts[1])
		if err != nil {
			response.ErrorResponse(c, "认证已过期或无效", 4001)
			c.Abort()
			return
		}

		if claims.Type != AccessToken {
			response.ErrorResponse(c, "Token类型错误", 4001)
			c.Abort()
			return
		}

		// Set user ID in context for downstream handlers
		c.Set("user_id", claims.UserID)
		c.Set("token_raw", parts[1])
		c.Next()
	}
}
