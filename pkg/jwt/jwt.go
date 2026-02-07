package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 定义加密的密钥 (生产环境应该从配置文件读取)
var jwtKey = []byte("shouguang_vegetable_secret_key_2026")

type Claims struct {
	UserId   int64  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken 生成 Token
func GenerateToken(userId int64, username string) (string, error) {
	expirationTime := time.Now().Add(24 * time.Hour) // 24小时有效期
	claims := &Claims{
		UserId:   userId,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			Issuer:    "go-ecommerce",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtKey)
}

// ParseToken 解析 Token
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
