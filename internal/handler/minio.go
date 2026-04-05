package handler

import (
	"CloudVault/config"
	"CloudVault/internal/activity"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/internal/task"
	"CloudVault/model"
	"CloudVault/utils"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
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

	if req.FileID != 0 { //  id ?id
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
	//  &&
	_ = service.RecordRecentAccess(userID, userFile.ID, "download")
	_ = activity.Emit(c.Request.Context(), userID, activity.ActionDownload, userFile.ID, written)
}

// MinioDownloadURL returns a user-scoped secure download URL.
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
	ticketTTL := config.AppConfig.DownloadTicketTTL
	if ticketTTL <= 0 {
		ticketTTL = 2 * time.Minute
	}
	token, err := service.CreateDownloadTicket(c.Request.Context(), userID, userFile.ID, ticketTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	url := buildSecureDownloadURL(c, token)

	c.JSON(http.StatusOK, gin.H{
		"url":                 url,
		"name":                name,
		"size":                fileObj.Size,
		"expires_in_seconds":  int(ticketTTL.Seconds()),
		"download_url_mode":   "secure",
		"download_url_scoped": "user",
	})
	_ = service.RecordRecentAccess(userID, userFile.ID, "download_url")
}

// SecureDownload streams a file via a download ticket.
func SecureDownload(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "download token required"})
		return
	}
	ticket, err := service.VerifyDownloadTicket(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	userID := ticket.UserID
	if userID == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "download token invalid"})
		return
	}
	if !service.CheckFileOwner(userID, ticket.FileID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "file not found"})
		return
	}
	userFile, err := service.GetUserFileById(ticket.FileID)
	if err != nil || userFile.ObjectID == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	fileObj, err := service.GetFileObjectById(*userFile.ObjectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
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

func buildSecureDownloadURL(c *gin.Context, token string) string {
	encoded := neturl.QueryEscape(token)
	path := "/api/file/download/secure?token=" + encoded
	scheme := "http"
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	} else if c.Request.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s", scheme, c.Request.Host, path)
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
	session, err := service.GetUploadSessionByUploadID(uploadID) //  session
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
	var session *model.UploadSession
	var err error
	if strings.TrimSpace(req.UploadID) != "" {
		session, err = service.GetUploadSessionByUploadID(req.UploadID)
	} else if strings.TrimSpace(req.FileHash) != "" {
		session, err = service.GetUploadSessionByHash(userID, req.FileHash)
	} else {
		c.JSON(400, gin.H{"msg": "upload_id or file_hash required"})
		return
	}
	if err != nil {
		c.JSON(404, gin.H{"msg": "upload session not found"})
		return
	}
	if session.UserID != userID {
		c.JSON(403, gin.H{"msg": "upload session forbidden"})
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
	if req.FileName == "" {
		req.FileName = session.FileName
	}
	req.UploadID = session.UploadID

	integrity, err := service.ValidateUploadChunks(c.Request.Context(), session.UploadID, req.TotalChunks)
	if err != nil {
		c.JSON(500, gin.H{"msg": err.Error()})
		return
	}
	if len(integrity.Missing) > 0 || len(integrity.Invalid) > 0 {
		msg := "chunks not complete"
		if len(integrity.Invalid) > 0 && len(integrity.Missing) == 0 {
			msg = "chunks invalid"
		}
		c.JSON(409, gin.H{
			"msg":            msg,
			"upload_id":      session.UploadID,
			"missing_chunks": integrity.Missing,
			"invalid_chunks": integrity.Invalid,
		})
		return
	}
	lockKey := "lock:merge:" + strconv.FormatUint(userID, 10) + ":" + session.UploadID
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
	hash, err := service.CompleteFileWithHash(
		c.Request.Context(),
		req,
		userName,
	)
	if err != nil {
		if err.Error() == "chunks not complete" || err.Error() == "chunks invalid" {
			integrity, missErr := service.ValidateUploadChunks(c.Request.Context(), session.UploadID, req.TotalChunks)
			if missErr != nil {
				c.JSON(500, gin.H{"msg": missErr.Error()})
				return
			}
			msg := "chunks not complete"
			if len(integrity.Invalid) > 0 && len(integrity.Missing) == 0 {
				msg = "chunks invalid"
			}
			c.JSON(409, gin.H{
				"msg":            msg,
				"upload_id":      session.UploadID,
				"missing_chunks": integrity.Missing,
				"invalid_chunks": integrity.Invalid,
			})
			return
		}
		mergeMsg := task.MergeMessage{
			UploadID: session.UploadID,
			UserID:   userID,
			UserName: userName,
			Request:  req,
			Attempt:  0,
		}
		if qErr := task.EnqueueMergeTask(c.Request.Context(), mergeMsg); qErr != nil {
			c.JSON(500, gin.H{"msg": "merge failed and queue failed: " + qErr.Error()})
			return
		}
		c.JSON(202, gin.H{
			"msg":       "merge queued",
			"upload_id": session.UploadID,
			"reason":    err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"msg":       "upload completed",
		"upload_id": session.UploadID,
		"hash":      hash,
	})
}
