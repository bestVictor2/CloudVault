package handler

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/service"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AskAI handles AI assistant requests.
func AskAI(c *gin.Context) {
	var req dto.AIAskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question required"})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	resp, err := service.AskAI(c.Request.Context(), userID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
