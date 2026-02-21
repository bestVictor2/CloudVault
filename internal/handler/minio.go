package handler

import (
	"Go_Pan/config"
	"Go_Pan/internal/activity"
	"Go_Pan/internal/dto"
	"Go_Pan/internal/repo"
	"Go_Pan/internal/service"
	"Go_Pan/model"
	"Go_Pan/utils"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// MinioDownloadFile streams a file from MinIO.
func MinioDownloadFile(c *gin.Context) {
	var req dto.MinioDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "download failed: " + err.Error()})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	var (
		userFile *model.UserFile
		fileObj  *model.FileObject
		err      error
	)

	if req.FileID != 0 {
		if !service.CheckFileOwner(userID, req.FileID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "file not found"})
			return
		}
		userFile, err = service.GetUserFileById(req.FileID)
		if err != nil || userFile.ObjectID == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		fileObj, err = service.GetFileObjectById(*userFile.ObjectID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
	} else {
		hash := strings.TrimSpace(req.FileHash)
		if hash == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file_id or file_hash required"})
			return
		}
		fileObj, err = service.GetFileObjectByHash(hash)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		userFile, err = service.GetUserFileByObjectID(userID, fileObj.ID)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "file not found"})
			return
		}
	}

	object, info, err := service.MinioDownloadObject(
		c.Request.Context(),
		fileObj.ObjectName,
	)
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	defer object.Close()

	fileName := userFile.Name
	if fileName == "" {
		fileName = path.Base(info.ObjectName)
	}
	fileName = utils.SanitizeHeaderFilename(fileName)
	contentType := service.GetContentBook(fileName)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header(
		"Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"", fileName),
	)
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size))

	written, err := io.Copy(c.Writer, object)
	if err != nil {
		log.Println("download error:", err)
		return
	}
	_ = service.RecordRecentAccess(userID, userFile.ID, "download")
	_ = activity.Emit(c.Request.Context(), userID, activity.ActionDownload, userFile.ID, written)
}

// MinioDownloadURL returns a presigned download URL for MinIO.
func MinioDownloadURL(c *gin.Context) {
	var req dto.MinioDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "download failed: " + err.Error()})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	var (
		userFile *model.UserFile
		fileObj  *model.FileObject
		err      error
	)

	if req.FileID != 0 {
		if !service.CheckFileOwner(userID, req.FileID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "file not found"})
			return
		}
		userFile, err = service.GetUserFileById(req.FileID)
		if err != nil || userFile.ObjectID == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		fileObj, err = service.GetFileObjectById(*userFile.ObjectID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
	} else {
		hash := strings.TrimSpace(req.FileHash)
		if hash == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file_id or file_hash required"})
			return
		}
		fileObj, err = service.GetFileObjectByHash(hash)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		userFile, err = service.GetUserFileByObjectID(userID, fileObj.ID)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "file not found"})
			return
		}
	}

	name := userFile.Name
	if name == "" {
		name = path.Base(fileObj.ObjectName)
	}
	url, err := service.GetDownloadURL(c.Request.Context(), fileObj.BucketName, fileObj.ObjectName, name, 10*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"url":  url,
		"name": name,
		"size": fileObj.Size,
	})
	_ = service.RecordRecentAccess(userID, userFile.ID, "download_url")
}

// MultiPartFileInit initializes multipart upload.
func MultiPartFileInit(c *gin.Context) {
	var req dto.MultipartInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"msg": err.Error()})
		return
	}
	req.UserId = c.MustGet("user_id").(uint64)
	resp, err := service.MultiPartFileInit(c.Request.Context(), req)
	if err != nil {
		utils.Fail(c, err)
		return
	}
	utils.Success(c, resp)
}

// MultipartUploadChunk uploads a file chunk.
func MultipartUploadChunk(c *gin.Context) {
	chunkIndex, err := strconv.Atoi(c.PostForm("chunk_index"))
	if err != nil {
		c.JSON(400, gin.H{"msg": "invalid chunk_index"})
		return
	}
	uploadID := c.PostForm("upload_id")
	if uploadID == "" {
		c.JSON(400, gin.H{"msg": "missing upload_id"})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	session, err := service.GetUploadSessionByUploadID(uploadID)
	if err != nil {
		c.JSON(404, gin.H{"msg": "upload session not found"})
		return
	}
	if session.UserID != userID {
		c.JSON(403, gin.H{"msg": "upload session forbidden"})
		return
	}
	if chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		c.JSON(400, gin.H{"msg": "chunk index out of range"})
		return
	}
	file, err := c.FormFile("chunk")
	if err != nil {
		c.JSON(400, gin.H{"msg": "missing chunk"})
		return
	}
	req := &dto.MultipartUploadChunkRequest{
		UploadID:   uploadID,
		BucketName: config.AppConfig.BucketName,
		ChunkIndex: chunkIndex,
		File:       file,
	}
	if err := service.UploadChunk(
		c.Request.Context(),
		req,
	); err != nil {
		c.JSON(500, gin.H{"msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"msg": "ok"})
}

// MultipartComplete completes multipart upload.
func MultipartComplete(c *gin.Context) {
	var req dto.MultipartCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"msg": err.Error()})
		return
	}
	value, _ := c.Get("username")
	userName, _ := value.(string)
	userID := c.MustGet("user_id").(uint64)
	session, err := service.GetUploadSessionByHash(userID, req.FileHash)
	if err != nil {
		c.JSON(404, gin.H{"msg": "upload session not found"})
		return
	}
	if req.TotalChunks <= 0 {
		req.TotalChunks = session.TotalChunks
	} else if req.TotalChunks != session.TotalChunks {
		c.JSON(400, gin.H{"msg": "total_chunks mismatch"})
		return
	}
	if req.FileSize <= 0 && session.FileSize > 0 {
		req.FileSize = session.FileSize
	}
	lockKey := "lock:merge:" + strconv.FormatUint(userID, 10) + ":" + req.FileHash
	lock := repo.NewRedisLock(
		repo.Redis,
		lockKey,
		30*time.Second,
	)
	ctx := c.Request.Context()
	if err := lock.Lock(ctx); err != nil {
		c.JSON(500, gin.H{"msg": "lock failed: " + err.Error()})
		return
	}
	defer lock.Unlock(ctx)
	if err := service.CompleteFile(
		c.Request.Context(),
		req,
		userName,
	); err != nil {
		c.JSON(500, gin.H{"msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"msg": "upload completed"})
}
