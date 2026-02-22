package service

import (
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"CloudVault/utils"
	"context"
	"errors"
	"fmt"
	"time"
)

// GetPreviewURL generates a presigned preview URL.
func GetPreviewURL(ctx context.Context, userID, fileID uint64, expiry time.Duration) (string, error) {
	var file model.UserFile
	if err := repo.Db.Where("id = ? AND user_id = ? AND is_deleted = 0", fileID, userID).First(&file).Error; err != nil {
		return "", err
	}
	if file.ObjectID == nil {
		return "", errors.New("file not found")
	}

	var obj model.FileObject
	if err := repo.Db.Where("id = ?", *file.ObjectID).First(&obj).Error; err != nil {
		return "", err
	}

	if storage.Default == nil {
		return "", errors.New("storage not initialized")
	}
	contentType := GetContentBook(file.Name)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	safeName := utils.SanitizeHeaderFilename(file.Name)
	disposition := fmt.Sprintf("inline; filename=\"%s\"", safeName)
	url, err := storage.Default.PresignedGetObjectWithResponse(
		ctx,
		obj.BucketName,
		obj.ObjectName,
		expiry,
		map[string]string{
			"response-content-type":        contentType,
			"response-content-disposition": disposition,
		},
	)
	if err != nil {
		return "", err
	}
	return url, nil
}

