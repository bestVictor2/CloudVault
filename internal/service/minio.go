package service

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"CloudVault/utils"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"gorm.io/gorm"
)

// MinioUploadFile uploads a file to MinIO.
func MinioUploadFile(
	ctx context.Context,
	userId uint64,
	bucketName string,
	objectName string,
	reader io.Reader,
	size int64,
	filePath string,
	hash string,
) error {
	var existingObj model.FileObject
	err := repo.Db.Where("bucket_name = ? AND object_name = ?", bucketName, objectName).
		First(&existingObj).Error
	var objectID uint64
	createdNew := false

	if err == nil {
		available, checkErr := isFileObjectAvailable(ctx, &existingObj)
		if checkErr != nil {
			return checkErr
		}
		if !available {
			if storage.Default == nil {
				return fmt.Errorf("storage not initialized")
			}
			if err := storage.Default.PutObject(
				ctx,
				bucketName,
				objectName,
				reader,
				size,
				storage.PutOptions{
					ContentType: GetContentBook(filePath),
				},
			); err != nil {
				return err
			}
			if err := repo.Db.Model(&model.FileObject{}).
				Where("id = ?", existingObj.ID).
				Updates(map[string]interface{}{
					"bucket_name": bucketName,
					"object_name": objectName,
					"size":        size,
				}).Error; err != nil {
				return err
			}
		}
		if err := IncreaseRefCount(existingObj.ID); err != nil {
			return err
		}
		objectID = existingObj.ID
	} else if err != gorm.ErrRecordNotFound {
		return err
	} else {
		if storage.Default == nil {
			return fmt.Errorf("storage not initialized")
		}
		if err := storage.Default.PutObject(
			ctx,
			bucketName,
			objectName,
			reader,
			size,
			storage.PutOptions{
				ContentType: GetContentBook(filePath),
			},
		); err != nil {
			return err
		}
		fileObject := &model.FileObject{
			UserID:     userId,
			BucketName: bucketName,
			ObjectName: objectName,
			Size:       size,
			Hash:       hash,
			RefCount:   1,
		}
		if err := CreateFilesObject(fileObject); err != nil {
			return err
		}
		objectID = fileObject.ID
		createdNew = true
	}

	file := &model.UserFile{
		UserID:   userId,
		ObjectID: &objectID,
		Name:     path.Base(filePath),
		IsDir:    false,
		Size:     size,
	}
	if err := CreateUserFileEntry(file); err != nil {
		if createdNew {
			if storage.Default != nil {
				_ = storage.Default.RemoveObject(ctx, bucketName, objectName)
			}
			_ = repo.Db.Delete(&model.FileObject{}, objectID).Error
		} else {
			_, _ = DecreaseRefCount(objectID)
		}
		return err
	}
	return nil
}

// GetContentBook returns content type by file extension.
func GetContentBook(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// MinioDownloadObject downloads an object from MinIO.
func MinioDownloadObject(
	ctx context.Context,
	objectName string,
) (io.ReadCloser, *storage.ObjectInfo, error) {
	if objectName == "" {
		return nil, nil, fmt.Errorf("object name missing")
	}
	if storage.Default == nil {
		return nil, nil, fmt.Errorf("storage not initialized")
	}
	object, info, err := storage.Default.GetObject(ctx, config.AppConfig.BucketName, objectName)
	if err != nil {
		return nil, nil, err
	}
	return object, &info, nil
}

// GetDownloadURL returns a presigned download URL for a MinIO object.
func GetDownloadURL(
	ctx context.Context,
	bucketName string,
	objectName string,
	fileName string,
	expiry time.Duration,
) (string, error) {
	if objectName == "" {
		return "", fmt.Errorf("object name missing")
	}
	if storage.Default == nil {
		return "", fmt.Errorf("storage not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	contentType := GetContentBook(fileName)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	safeName := utils.SanitizeHeaderFilename(fileName)
	disposition := fmt.Sprintf("attachment; filename=\"%s\"", safeName)
	url, err := storage.Default.PresignedGetObjectWithResponse(
		ctx,
		bucketName,
		objectName,
		expiry,
		map[string]string{
			"response-content-type":        contentType,
			"response-content-disposition": disposition,
		},
	)
	if err == nil {
		return url, nil
	}
	return storage.Default.PresignedGetObject(ctx, bucketName, objectName, expiry)
}

// MinioDownloadFile downloads a file by hash, keeping compatibility with older tests.
func MinioDownloadFile(
	ctx context.Context,
	username string,
	hash string,
) (io.ReadCloser, *storage.ObjectInfo, error) {
	if hash == "" {
		return nil, nil, fmt.Errorf("file hash missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	obj, err := GetFileObjectByHash(hash)
	if err != nil {
		return nil, nil, err
	}
	objectName := obj.ObjectName
	if objectName == "" && username != "" {
		objectName = BuildObjectName(username, hash)
	}
	return MinioDownloadObject(ctx, objectName)
}

// HTTPStatusError is returned for non-200 HTTP responses.
type HTTPStatusError struct {
	StatusCode int
	Status     string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("bad status: %s", e.Status)
}

func hostAllowed(host string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	host = strings.ToLower(strings.TrimSpace(host))
	for _, entry := range allowlist {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, ".") {
			if strings.HasSuffix(host, entry) {
				return true
			}
			continue
		}
		if host == entry {
			return true
		}
	}
	return false
}

func isLocalHostname(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || host == "localhost.localdomain" {
		return true
	}
	if strings.HasSuffix(host, ".local") {
		return true
	}
	return false
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	return false
}

func validateDownloadURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("missing host")
	}
	if !hostAllowed(host, config.AppConfig.DownloadAllowedHosts) {
		return nil, fmt.Errorf("host not allowed")
	}
	if config.AppConfig.DownloadAllowPrivate {
		return u, nil
	}
	if isLocalHostname(host) {
		return nil, fmt.Errorf("host not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("ip not allowed")
		}
		return u, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("host not resolvable")
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("ip not allowed")
		}
	}
	return u, nil
}

// ValidateDownloadSourceURL validates an offline-download source URL before task creation.
func ValidateDownloadSourceURL(rawURL string) error {
	_, err := validateDownloadURL(rawURL)
	return err
}

// DownloadByHTTP downloads a URL into MinIO.
func DownloadByHTTP(ctx context.Context, rawURL string, fileName string, userId uint64) (int64, error) {
	parsed, err := validateDownloadURL(rawURL)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{
		Timeout: config.AppConfig.DownloadHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			_, err := validateDownloadURL(req.URL.String())
			return err
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}
	if config.AppConfig.DownloadMaxBytes > 0 {
		if resp.ContentLength < 0 {
			return 0, fmt.Errorf("unknown content length")
		}
		if resp.ContentLength > config.AppConfig.DownloadMaxBytes {
			return 0, fmt.Errorf("content too large")
		}
	}
	userName, err := FindUserNameById(userId)
	if err != nil {
		return 0, err
	}
	if storage.Default == nil {
		return 0, fmt.Errorf("storage not initialized")
	}
	if err := storage.Default.PutObject(
		ctx,
		config.AppConfig.BucketName,
		BuildObjectName(userName, fileName),
		resp.Body,
		resp.ContentLength,
		storage.PutOptions{
			ContentType: resp.Header.Get("Content-Type"),
		},
	); err != nil {
		return 0, err
	}
	return resp.ContentLength, nil
}

// UploadFromURL downloads a remote file into MinIO and creates user/file-object records.
func UploadFromURL(
	ctx context.Context,
	userID uint64,
	rawURL string,
	fileName string,
	parentID *uint64,
) (*model.UserFile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ValidateDownloadSourceURL(rawURL); err != nil {
		return nil, err
	}
	fileHash := utils.GetToken()
	size, err := DownloadByHTTP(ctx, rawURL, fileHash, userID)
	if err != nil {
		return nil, err
	}
	userName, err := FindUserNameById(userID)
	if err != nil {
		return nil, err
	}
	objectName := BuildObjectName(userName, fileHash)
	removeObject := func() {
		if storage.Default != nil {
			_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, objectName)
		}
	}

	fileObj := &model.FileObject{
		UserID:     userID,
		Hash:       fileHash,
		BucketName: config.AppConfig.BucketName,
		ObjectName: objectName,
		Size:       size,
		RefCount:   1,
	}
	if err := CreateFilesObject(fileObj); err != nil {
		removeObject()
		return nil, err
	}

	userFile := &model.UserFile{
		UserID:   userID,
		ParentID: parentID,
		Name:     fileName,
		IsDir:    false,
		ObjectID: &fileObj.ID,
		Size:     size,
	}
	if err := CreateUserFileEntry(userFile); err != nil {
		removeObject()
		_ = repo.Db.Delete(&model.FileObject{}, fileObj.ID).Error
		return nil, err
	}
	return userFile, nil
}
