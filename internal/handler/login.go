package handler

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/service"
	"CloudVault/model"
	"CloudVault/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Login authenticates a user and returns a token.
func Login(c *gin.Context) {
	var loginRequest dto.LoginRequest
	if err := c.ShouldBind(&loginRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误" + err.Error()})
		return
	}
	var user *model.User
	var err error
	if user, err = service.IsExist(loginRequest.Username); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该用户不存在"})
		return
	}
	if !user.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号未激活"})
		return
	}
	if err = service.CheckPassword(loginRequest.Username, loginRequest.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "密码错误"})
		return
	}
	token, err := utils.GenerateToken(user.ID, user.UserName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tokens"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"token":   token,
		"user":    user,
	})
}
