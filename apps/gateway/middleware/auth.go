package middleware

import (
	"net/http"
	"strings"

	"go-ecommerce/pkg/response" // [新增] 引入响应包

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret = []byte("my_secret_key")

func AuthMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authHeader := ctx.GetHeader("Authorization")
		if authHeader == "" {
			// [修改] 使用统一错误响应
			response.Error(ctx, http.StatusUnauthorized, "Authorization header is missing")
			ctx.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.Error(ctx, http.StatusUnauthorized, "Invalid authorization format")
			ctx.Abort()
			return
		}
		tokenString := parts[1]

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			response.Error(ctx, http.StatusUnauthorized, "Invalid token")
			ctx.Abort()
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if userIdFloat, ok := claims["user_id"].(float64); ok {
				ctx.Set("userId", int64(userIdFloat))
			} else {
				response.Error(ctx, http.StatusUnauthorized, "Invalid token claims")
				ctx.Abort()
				return
			}
		}

		ctx.Next()
	}
}
