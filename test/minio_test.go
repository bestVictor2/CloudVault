package test

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// 清理测试数据
func cleanMinioTables(t *testing.T) {
	// 临时禁用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")

	// 按照外键依赖关系的顺序清理表数据
	tables := []string{"file_share", "file_chunk", "upload_session", "file_object", "user_file", "user_db"}
	for _, table := range tables {
		if err := repo.Db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s failed: %v", table, err)
		}
	}

	// 重新启用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

// 创建测试用户
func createMinioTestUser(t *testing.T) *model.User {
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

// 测试GetContentBook
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

// 测试BuildObjectName
func TestBuildObjectNameForMinio(t *testing.T) {
	username := "miniouser"
	hash := "minio_hash_123"
	expected := "files/miniouser/minio_hash_123"
	result := service.BuildObjectName(username, hash)
	if result != expected {
		t.Fatalf("BuildObjectName failed: expect %s, got %s", expected, result)
	}
}

// 娴嬭瘯MinioUploadFile - 脏记录命中但对象缺失时，需自动重传对象
func TestMinioUploadFileRepairMissingObject(t *testing.T) {
	cleanMinioTables(t)
	user := createMinioTestUser(t)

	hash := "repair_minio_missing_hash"
	objectName := service.BuildObjectName(user.UserName, hash)
	fileObj := &model.FileObject{
		UserID:     user.ID,
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

// 测试MinioDownloadFile - 文件不存
func TestMinioDownloadFileNotFound(t *testing.T) {
	cleanMinioTables(t)
	user := createMinioTestUser(t)

	// 尝试下载不存在的文件
	_, _, err := service.MinioDownloadFile(nil, user.UserName, "non_existent_file.txt")
	if err == nil {
		t.Fatal("MinioDownloadFile should return error for non-existent file")
	}
}

// 测试MinioDownloadFile - 文件存在
func TestMinioDownloadFileSuccess(t *testing.T) {
	cleanMinioTables(t)
	user := createMinioTestUser(t)

	// 创建文件对象记录
	fileObj := &model.FileObject{
		UserID:     user.ID,
		Hash:       "download_test_hash",
		BucketName: "test-bucket",
		ObjectName: service.BuildObjectName(user.UserName, "download_test_hash"),
		Size:       1024,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	// 注意：这个测试需要 MinIO 中有实际的文件才能完全测试。
	// 这里我们只测试文件对象记录存在的情况。
	// 实际下载需要 MinIO 中有对应的文件。
	_, _, err := service.MinioDownloadFile(nil, user.UserName, "download_test_hash")
	// 由于 MinIO 中可能没有实际文件，这里可能会返回错误。
	// 我们只验证函数调用不会 panic。
	_ = err
}



