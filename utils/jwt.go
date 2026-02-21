package utils

import (
	"Go_Pan/config"
	"errors"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	UserId   uint64 `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken creates a JWT.
func GenerateToken(userId uint64, username string) (string, error) {
	claims := Claims{
		UserId:   userId,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)                 // 生成一个 jwt 对象
	tokenString, err := token.SignedString([]byte(config.AppConfig.JWTSecret)) // 使用密钥进行加密生成返回字符串
	if err != nil {
		log.Println("Error signing token:", err)
		return "", err
	}
	return tokenString, nil
}

// VerifyToken parses and validates a JWT.
func VerifyToken(tokenString string) (*Claims, error) {
	// 使用回调函数
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(config.AppConfig.JWTSecret), nil // 要求使用该密钥 传入函数的作用就在此处
	})
	if err != nil {
		log.Println("Error parsing token:", err)
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token")
}
