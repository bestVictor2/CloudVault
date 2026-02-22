package handler

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ListRecycleFiles 查看回收站列
func ListRecycleFiles(c *gin.Context) {
	userID := c.MustGet("user_id").(uint64)
	files, err := service.ListRecycleFiles(uint(userID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get recycle files failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"files": files,
	})
}

// RestoreFile 恢复文件
func RestoreFile(c *gin.Context) {
	var req dto.RestoreFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	userID := c.MustGet("user_id").(uint64)
	if err := service.RestoreFile(uint(userID), uint(req.FileID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "restore file failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}

// DeleteFileRecord 彻底删除文件
func DeleteFileRecord(c *gin.Context) {
	var req dto.DeleteFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	userID := c.MustGet("user_id").(uint64)
	if err := service.DeleteFileRecord(uint(userID), uint(req.FileID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete file failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}



