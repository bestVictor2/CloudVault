package handler

import (
	"Go_Pan/internal/dto"
	"Go_Pan/internal/repo"
	"Go_Pan/internal/service"
	"Go_Pan/model"
	"Go_Pan/utils"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

// Register starts user registration and sends activation mail.
func Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.FirstPassword != req.LastPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "passwords do not match"})
		return
	}
	if _, err := service.IsExist(req.Username); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username already exists"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "check username failed"})
		return
	}
	if err := service.IsEmailExist(req.Email); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email already exists"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "check email failed"})
		return
	}

	token := uuid.NewString()
	key := "register:" + token
	tmp := struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		Email:    req.Email,
		Username: req.Username,
		Password: req.FirstPassword,
	}

	data, _ := json.Marshal(tmp)
	if err := repo.Redis.Set(context.Background(), key, data, 10*time.Minute).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cache activation token failed: " + err.Error()})
		return
	}

	link := buildActivateLink(c, token)
	if err := utils.SendActivateMail(req.Email, link); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "send activation email failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "activation email sent"})
}

func buildActivateLink(c *gin.Context, token string) string {
	baseURL := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if baseURL == "" {
		scheme := "http"
		if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
			scheme = forwarded
		} else if c.Request.TLS != nil {
			scheme = "https"
		}
		host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
		if host == "" {
			host = c.Request.Host
		}
		baseURL = scheme + "://" + host
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return baseURL + "/api/activate?token=" + url.QueryEscape(token)
}

// Activate activates a user account.
func Activate(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "token missing"})
		return
	}

	key := "register:" + token
	ctx := context.Background()
	val, err := repo.Redis.Get(ctx, key).Result()
	if err != nil {
		usedKey := "register_used:" + token
		if used, usedErr := repo.Redis.Get(ctx, usedKey).Result(); usedErr == nil && used == "1" {
			c.JSON(http.StatusOK, gin.H{"msg": "account already activated"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"msg": "link invalid or expired"})
		return
	}

	var tmp struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal([]byte(val), &tmp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "decode failed"})
		return
	}

	user := model.User{
		Email:    tmp.Email,
		UserName: tmp.Username,
		Password: tmp.Password,
		IsActive: true,
	}
	if err := service.CreateUser(&user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": err.Error()})
		return
	}

	repo.Redis.Del(ctx, key)
	_ = repo.Redis.Set(ctx, "register_used:"+token, "1", 10*time.Minute).Err()
	c.JSON(http.StatusOK, gin.H{"msg": "account activated"})
}
