package handler

import (
	"CloudVault/internal/activity"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// GetUserActivitySummary returns user daily activity stats.
func GetUserActivitySummary(c *gin.Context) {
	days := 7
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid days"})
			return
		}
		days = parsed
	}

	userID := c.MustGet("user_id").(uint64)
	items, err := activity.GetSummary(c.Request.Context(), userID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get activity summary failed: " + err.Error()})
		return
	}

	var totalUploadBytes int64
	var totalDeleteBytes int64
	var totalDownloadBytes int64
	var totalUploadCount int64
	var totalDeleteCount int64
	var totalShareCount int64
	var totalDownloadCount int64
	for _, item := range items {
		totalUploadBytes += item.UploadBytes
		totalDeleteBytes += item.DeleteBytes
		totalDownloadBytes += item.DownloadBytes
		totalUploadCount += item.UploadCount
		totalDeleteCount += item.DeleteCount
		totalShareCount += item.ShareCount
		totalDownloadCount += item.DownloadCount
	}

	c.JSON(http.StatusOK, gin.H{
		"days":                 days,
		"items":                items,
		"total_upload_count":   totalUploadCount,
		"total_upload_bytes":   totalUploadBytes,
		"total_delete_count":   totalDeleteCount,
		"total_delete_bytes":   totalDeleteBytes,
		"total_share_count":    totalShareCount,
		"total_download_count": totalDownloadCount,
		"total_download_bytes": totalDownloadBytes,
	})
}
