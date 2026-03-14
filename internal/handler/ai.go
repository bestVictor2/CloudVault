package handler

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/service"
	"net/http"
	"strconv"
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

// AskAIRAG handles retrieval-augmented AI QA requests.
func AskAIRAG(c *gin.Context) {
	var req dto.AIRAGAskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question required"})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	resp, err := service.AskAIRAG(c.Request.Context(), userID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetAIHistory returns persisted chat history for current user.
func GetAIHistory(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	items, err := service.GetAIHistory(c.Request.Context(), userID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ClearAIHistory clears persisted chat history for current user.
func ClearAIHistory(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	if err := service.ClearAIHistory(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}

// 传入 context 是为了假如关闭了 http 链接 那么直接调用 done 函数 避免 ai 的 api 接口一直浪费资源
