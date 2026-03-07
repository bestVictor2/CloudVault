package service

import (
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"context"
	"errors"
	"time"
)

// GetPreviewURL generates a preview URL.
// When preview transcoding is enabled and the file is a non-web-friendly video format,
// the file is transcoded to mp4 before returning a URL.
func GetPreviewURL(ctx context.Context, userID, fileID uint64, expiry time.Duration) (string, error) {
	file, obj, err := loadPreviewFileAndObject(userID, fileID)
	if err != nil {
		return "", err
	}
	if shouldTranscodeForPreview(file.Name) && configPreviewTranscodeEnabled() {
		return getOrBuildTranscodedPreviewURL(ctx, file, obj, expiry)
	}
	return buildInlinePreviewURL(ctx, obj.BucketName, obj.ObjectName, file.Name, expiry)
}

func loadPreviewFileAndObject(userID, fileID uint64) (*model.UserFile, *model.FileObject, error) {
	var file model.UserFile
	if err := repo.Db.Where("id = ? AND user_id = ? AND is_deleted = 0", fileID, userID).First(&file).Error; err != nil {
		return nil, nil, err
	}
	if file.ObjectID == nil {
		return nil, nil, errors.New("file not found")
	}

	var obj model.FileObject
	if err := repo.Db.Where("id = ?", *file.ObjectID).First(&obj).Error; err != nil {
		return nil, nil, err
	}
	return &file, &obj, nil
}

func buildInlinePreviewURL(ctx context.Context, bucketName, objectName, fileName string, expiry time.Duration) (string, error) {
	if storage.Default == nil {
		return "", errors.New("storage not initialized")
	}
	contentType := GetContentBook(fileName)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	disposition := buildInlineDisposition(fileName)
	return storage.Default.PresignedGetObjectWithResponse(
		ctxOrBackground(ctx),
		bucketName,
		objectName,
		expiry,
		map[string]string{
			"response-content-type":        contentType,
			"response-content-disposition": disposition,
		},
	)
}
