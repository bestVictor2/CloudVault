package handler

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/service"
	"CloudVault/internal/storage"
	"CloudVault/internal/task"
	"CloudVault/utils"
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// UploadFileByHash handles hash-based instant upload.
func UploadFileByHash(c *gin.Context) {
	var req dto.UploadFileByHashRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	req.UserId = c.MustGet("user_id").(uint64)
	resp, err := service.FastUpload(
		c.Request.Context(),
		&req,
	)
	if err != nil {
		utils.Fail(c, err)
		return
	}
	utils.Success(c, resp)
}

// HttpOfflineDownload creates an offline download task.
func HttpOfflineDownload(c *gin.Context) {
	var req dto.HttpOfflineDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := service.ValidateDownloadSourceURL(req.URL); err != nil { // 校验下载的 url 是否合法
		msg := err.Error()
		if msg == "host not allowed" || msg == "ip not allowed" {
			msg = msg + "; for local/private testing set DOWNLOAD_ALLOW_PRIVATE=true"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	value, _ := c.Get("user_id")
	userID, _ := value.(uint64)
	downloadTask, err := task.CreateDownloadTask(userID, req.URL, req.FileName)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"msg": "download task created", "task_id": downloadTask.ID})
}

// UploadFileByURL downloads a URL and stores it in MinIO as a user file.
func UploadFileByURL(c *gin.Context) {
	var req dto.URLUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	fileName := strings.TrimSpace(req.FileName) // trimspace 函数作用移除前后空白字符
	if fileName == "" {
		fileName = inferFileNameFromURL(req.URL) // 从 url 中提取
	}
	if fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_name required"})
		return
	}
	var parentID *uint64
	if req.ParentID != 0 {
		parentID = &req.ParentID
	}
	file, err := service.UploadFromURL(c.Request.Context(), userID, req.URL, fileName, parentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"msg":       "ok",
		"file_id":   file.ID,
		"name":      file.Name,
		"size":      file.Size,
		"parent_id": file.ParentID,
	})
}

func inferFileNameFromURL(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	base := strings.TrimSpace(path.Base(parsed.Path))
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return base
}

// DownloadArchive downloads multiple files or folders as a zip archive.
func DownloadArchive(c *gin.Context) {
	var req dto.ArchiveDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if len(req.FileIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_ids required"})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	entries, err := service.BuildArchiveEntries(userID, req.FileIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	name := req.Name
	if name == "" {
		name = "archive.zip"
	}
	name = utils.SanitizeHeaderFilename(name)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", name))
	c.Header("Content-Type", "application/zip")

	zipWriter := zip.NewWriter(c.Writer)
	defer zipWriter.Close()

	for _, entry := range entries {
		if entry.IsDir {
			if _, err := zipWriter.Create(entry.ZipPath); err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			continue
		}
		if entry.FileObj == nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if storage.Default == nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		object, _, err := storage.Default.GetObject( // 获取文件
			c.Request.Context(),
			entry.FileObj.BucketName,
			entry.FileObj.ObjectName,
		)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		writer, err := zipWriter.Create(entry.ZipPath) // 写入路径信息
		if err != nil {
			_ = object.Close()
			c.Status(http.StatusInternalServerError)
			return
		}
		if _, err := io.Copy(writer, object); err != nil { // 流式写入
			_ = object.Close()
			c.Status(http.StatusInternalServerError)
			return
		}
		_ = object.Close()
	}
}

// GetFileList returns a user's file list.
func GetFileList(c *gin.Context) {
	var req dto.FileListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	userID := c.MustGet("user_id").(uint64)
	files, total, err := service.GetFileList(userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get file list failed: " + err.Error()})
		return
	}
	if req.ParentID != nil && *req.ParentID != 0 {
		_ = service.RecordRecentAccess(userID, *req.ParentID, "browse")
	}

	c.JSON(http.StatusOK, gin.H{
		"files":     files,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
	})
}

// RenameFile updates a file or folder name.
func RenameFile(c *gin.Context) {
	var req dto.FileRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	userID := c.MustGet("user_id").(uint64)
	if err := service.RenameFile(userID, req.FileID, req.NewName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rename file failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}

// MoveFiles moves files or folders to a target.
func MoveFiles(c *gin.Context) {
	var req dto.FileMoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	userID := c.MustGet("user_id").(uint64)
	if err := service.MoveFiles(userID, req.FileIDs, req.TargetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "move files failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}

// CopyFiles copies files or folders to a target.
func CopyFiles(c *gin.Context) {
	var req dto.FileCopyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	userID := c.MustGet("user_id").(uint64)
	if err := service.CopyFiles(userID, req.FileIDs, req.TargetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "copy files failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}

// CreateFolder creates a folder.
func CreateFolder(c *gin.Context) {
	var req dto.FolderUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	userID := c.MustGet("user_id").(uint64)
	var parentID *uint64
	if req.ParentID != 0 {
		parentID = &req.ParentID
	}

	if err := service.CreateFolder(userID, parentID, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create folder failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}

// BatchDeleteFiles moves files to the recycle bin.
func BatchDeleteFiles(c *gin.Context) {
	var req dto.BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	userID := c.MustGet("user_id").(uint64)
	if err := service.BatchMoveToRecycle(userID, req.FileIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete files failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "success"})
}

// SearchFiles searches files by name.
func SearchFiles(c *gin.Context) {
	var req dto.FileSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	userID := c.MustGet("user_id").(uint64)
	files, total, err := service.SearchFiles(userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search files failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"files":     files,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
	})
}

// PreviewFile returns a preview URL for a file.
func PreviewFile(c *gin.Context) {
	fileIDStr := c.Param("fileID")
	fileID, err := strconv.ParseUint(fileIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}
	userID := c.MustGet("user_id").(uint64)
	url, err := service.GetPreviewURL(c.Request.Context(), userID, fileID, 10*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = service.RecordRecentAccess(userID, fileID, "preview")

	c.JSON(http.StatusOK, gin.H{"url": url})
}

// ListDownloadTasks lists download tasks for a user.
func ListDownloadTasks(c *gin.Context) {
	value, _ := c.Get("user_id")
	userID, _ := value.(uint64)
	tasks, err := task.ListDownloadTasks(userID, 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get tasks failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}
