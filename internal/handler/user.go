package handler

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/service"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetCurrentUser returns the current user's profile.
func GetCurrentUser(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	profile, err := service.GetUserProfileByID(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get profile failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, profile)
}

// UpdateCurrentUser updates editable profile fields.
func UpdateCurrentUser(c *gin.Context) {
	var req dto.UpdateUserProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	profile, err := service.UpdateUserProfile(userID, &req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, profile)
}

// ListUserFavorites lists favorite files/folders.
func ListUserFavorites(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	limit := parseIntQuery(c.Query("limit"), 50)
	items, err := service.ListFavorites(userID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list favorites failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AddUserFavorite adds one favorite.
func AddUserFavorite(c *gin.Context) {
	var req dto.FavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	if err := service.AddFavorite(userID, req.FileID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "add favorite failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"msg": "ok"})
}

// RemoveUserFavorite removes one favorite.
func RemoveUserFavorite(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	fileID, err := strconv.ParseUint(strings.TrimSpace(c.Param("fileID")), 10, 64)
	if err != nil || fileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}
	if err := service.RemoveFavorite(userID, fileID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "remove favorite failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"msg": "ok"})
}

// ListUserRecent lists recently accessed files/folders.
func ListUserRecent(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	limit := parseIntQuery(c.Query("limit"), 50)
	items, err := service.ListRecent(userID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list recent failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListUserCommonDirs lists frequently used directories.
func ListUserCommonDirs(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	limit := parseIntQuery(c.Query("limit"), 20)
	items, err := service.ListCommonDirs(userID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list common dirs failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func parseIntQuery(raw string, fallback int) int {
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
