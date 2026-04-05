package test

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func cleanMinioTables(t *testing.T) {
	t.Helper()
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")
	tables := []string{"file_share", "file_chunk", "upload_session", "file_object", "user_file", "user_db"}
	for _, table := range tables {
		if err := repo.Db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s failed: %v", table, err)
		}
	}
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

func createMinioTestUser(t *testing.T) *model.User {
	t.Helper()
	suffix := time.Now().UnixNano()
	user := &model.User{
		UserName: fmt.Sprintf("minio_test_user_%d", suffix),
		Password: "123456",
		Email:    fmt.Sprintf("minio_test_%d@test.com", suffix),
		IsActive: true,
	}
	if err := service.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	return user
}

func TestGetContentBook(t *testing.T) {
	testCases := []struct {
		filename string
		expected string
	}{
		{"test.jpg", "image/jpeg"},
		{"test.jpeg", "image/jpeg"},
		{"test.png", "image/png"},
		{"test.gif", "image/gif"},
		{"test.txt", "text/plain; charset=utf-8"},
		{"test.pdf", "application/pdf"},
		{"test.zip", "application/zip"},
		{"test.tar", "application/x-tar"},
		{"test.gz", "application/gzip"},
		{"test.mp4", "video/mp4"},
		{"test.unknown", "application/octet-stream"},
		{"TEST.JPG", "image/jpeg"},
		{"TEST.PNG", "image/png"},
	}

	for _, tc := range testCases {
		result := service.GetContentBook(tc.filename)
		if result != tc.expected {
			t.Fatalf("GetContentBook(%s) failed: expect %s, got %s", tc.filename, tc.expected, result)
		}
	}
}

func TestBuildObjectNameForMinio(t *testing.T) {
	hash := "minio_hash_123"
	expected := "files/sha256/mi/ni/minio_hash_123"
	result := service.BuildObjectName(hash)
	if result != expected {
		t.Fatalf("BuildObjectName failed: expect %s, got %s", expected, result)
	}
}

func TestMinioUploadFileRepairMissingObject(t *testing.T) {
	cleanMinioTables(t)
	user := createMinioTestUser(t)

	hash := "repair_minio_missing_hash"
	objectName := service.BuildObjectName(hash)
	fileObj := &model.FileObject{
		Hash:       hash,
		BucketName: config.AppConfig.BucketName,
		ObjectName: objectName,
		Size:       1,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	tmp, err := os.CreateTemp("", "CloudVault_minio_upload_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("repair-minio-upload")
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		t.Fatal(err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		_ = tmp.Close()
		t.Fatal(err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if err := service.MinioUploadFile(
		context.Background(),
		user.ID,
		config.AppConfig.BucketName,
		objectName,
		tmp,
		int64(len(content)),
		tmp.Name(),
		hash,
	); err != nil {
		t.Fatalf("MinioUploadFile failed: %v", err)
	}

	obj, _, err := storage.Default.GetObject(context.Background(), config.AppConfig.BucketName, objectName)
	if err != nil {
		t.Fatalf("uploaded object missing in minio: %v", err)
	}
	_ = obj.Close()
}

func TestMinioDownloadFileNotFound(t *testing.T) {
	cleanMinioTables(t)
	if _, _, err := service.MinioDownloadFile(nil, "", "non_existent_file.txt"); err == nil {
		t.Fatal("MinioDownloadFile should return error for non-existent file")
	}
}

func TestMinioDownloadFileSuccess(t *testing.T) {
	cleanMinioTables(t)

	hash := "download_test_hash"
	objectName := service.BuildObjectName(hash)
	fileObj := &model.FileObject{
		Hash:       hash,
		BucketName: config.AppConfig.BucketName,
		ObjectName: objectName,
		Size:       3,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}
	if storage.Default == nil {
		t.Fatal("storage not initialized")
	}
	if err := storage.Default.PutObject(
		context.Background(),
		config.AppConfig.BucketName,
		objectName,
		bytes.NewReader([]byte("abc")),
		3,
		storage.PutOptions{ContentType: "application/octet-stream"},
	); err != nil {
		t.Fatalf("put object failed: %v", err)
	}

	reader, _, err := service.MinioDownloadFile(nil, "", hash)
	if err != nil {
		t.Fatalf("MinioDownloadFile failed: %v", err)
	}
	_ = reader.Close()
}
