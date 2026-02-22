package handler

import (
	"CloudVault/internal/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// GetShareAccessLogs returns recent share access logs for current user.
func GetShareAccessLogs(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	limit := parsePositiveInt(c.Query("limit"), 50)
	shareID := strings.TrimSpace(c.Query("share_id"))

	items, err := service.ListShareAccessLogs(userID, shareID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list share access logs failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// GetShareAccessStats returns grouped share access stats for current user.
func GetShareAccessStats(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	days := parsePositiveInt(c.Query("days"), 30)

	stats, err := service.GetShareAccessStats(userID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get share access stats failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func parsePositiveInt(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
