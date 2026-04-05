package test

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

//

func cleanFileObjectTables(t *testing.T) {
	//
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")

	//
	tables := []string{"file_share", "file_chunk", "upload_session", "user_file", "file_object", "user_db"}
	for _, table := range tables {
		if err := repo.Db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s failed: %v", table, err)
		}
	}

	//
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

func createFileObjectTestUser(t *testing.T, prefix string) *model.User {
	t.Helper()
	suffix := time.Now().UnixNano()
	user := &model.User{
		UserName: fmt.Sprintf("%s_%d", prefix, suffix),
		Password: "123456",
		Email:    fmt.Sprintf("%s_%d@test.com", prefix, suffix),
		IsActive: true,
	}
	if err := service.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	return user
}

// BuildObjectName
func TestBuildObjectName(t *testing.T) {
	hash := "test_hash_123"
	expected := "files/sha256/te/st/test_hash_123"
	result := service.BuildObjectName(hash)
	if result != expected {
		t.Fatalf("BuildObjectName failed: expect %s, got %s", expected, result)
	}
}

// CreateFilesObject
func TestCreateFilesObject(t *testing.T) {
	cleanFileObjectTables(t)

	fileObj := &model.FileObject{
		Hash:       "test_hash_create",
		BucketName: "test-bucket",
		ObjectName: "test_object_create",
		Size:       2048,
		RefCount:   1,
	}

	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatalf("CreateFilesObject failed: %v", err)
	}

	if fileObj.ID == 0 {
		t.Fatal("file object ID should not be zero after create")
	}
}

// GetFileByObject
func TestGetFileByObject(t *testing.T) {
	cleanFileObjectTables(t)

	fileObj := &model.FileObject{
		Hash:       "test_hash_get",
		BucketName: "test-bucket",
		ObjectName: "test_object_get",
		Size:       4096,
		RefCount:   1,
	}

	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	// bucketobjectName
	found, err := service.GetFileByObject(fileObj.BucketName, fileObj.ObjectName)
	if err != nil {
		t.Fatalf("GetFileByObject failed: %v", err)
	}

	if found.ID != fileObj.ID {
		t.Fatalf("expect ID %d, got %d", fileObj.ID, found.ID)
	}
}

// GetFileObjectByHash
func TestGetFileObjectByHash(t *testing.T) {
	cleanFileObjectTables(t)

	fileObj := &model.FileObject{
		Hash:       "test_hash_by_hash",
		BucketName: "test-bucket",
		ObjectName: "test_object_by_hash",
		Size:       8192,
		RefCount:   1,
	}

	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	// hash
	found, err := service.GetFileObjectByHash(fileObj.Hash)
	if err != nil {
		t.Fatalf("GetFileObjectByHash failed: %v", err)
	}

	if found.ID != fileObj.ID {
		t.Fatalf("expect ID %d, got %d", fileObj.ID, found.ID)
	}
}

// GetFileObjectById
func TestGetFileObjectById(t *testing.T) {
	cleanFileObjectTables(t)

	fileObj := &model.FileObject{
		Hash:       "test_hash_by_id",
		BucketName: "test-bucket",
		ObjectName: "test_object_by_id",
		Size:       16384,
		RefCount:   1,
	}

	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	// ID
	found, err := service.GetFileObjectById(fileObj.ID)
	if err != nil {
		t.Fatalf("GetFileObjectById failed: %v", err)
	}

	if found.ID != fileObj.ID {
		t.Fatalf("expect ID %d, got %d", fileObj.ID, found.ID)
	}
}

// IncreaseRefCount
func TestIncreaseRefCount(t *testing.T) {
	cleanFileObjectTables(t)

	fileObj := &model.FileObject{
		Hash:       "test_hash_ref_count",
		BucketName: "test-bucket",
		ObjectName: "test_object_ref_count",
		Size:       32768,
		RefCount:   1,
	}

	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	//
	if err := service.IncreaseRefCount(fileObj.ID); err != nil {
		t.Fatalf("IncreaseRefCount failed: %v", err)
	}

	//
	found, err := service.GetFileObjectById(fileObj.ID)
	if err != nil {
		t.Fatal(err)
	}

	if found.RefCount != 2 {
		t.Fatalf("expect RefCount 2, got %d", found.RefCount)
	}
}

// FastUpload - ?
func TestFastUploadInstant(t *testing.T) {
	cleanFileObjectTables(t)

	user := createFileObjectTestUser(t, "fast_upload_user")

	//
	fileObj := &model.FileObject{
		Hash:       "fast_upload_hash",
		BucketName: config.AppConfig.BucketName,
		ObjectName: "test_object_fast",
		Size:       65536,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}
	_, err := storage.Minio.Client.PutObject(
		context.Background(),
		fileObj.BucketName,
		fileObj.ObjectName,
		bytes.NewReader([]byte("fast-upload-data")),
		int64(len("fast-upload-data")),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("put object failed: %v", err)
	}

	//
	req := &dto.UploadFileByHashRequest{
		UserId:   user.ID,
		ParentId: 0,
		FileName: "fast_upload_file.txt",
		Hash:     fileObj.Hash,
		IsDir:    false,
	}

	resp, err := service.FastUpload(nil, req)
	if err != nil {
		t.Fatalf("FastUpload failed: %v", err)
	}

	if !resp.Instant {
		t.Fatal("FastUpload should return Instant=true when file exists")
	}
	if resp.FileId == 0 {
		t.Fatal("FastUpload should return file_id when instant upload succeeds")
	}
}

// FastUpload -
func TestFastUploadNotInstant(t *testing.T) {
	cleanFileObjectTables(t)

	user := createFileObjectTestUser(t, "fast_upload_user2")

	//
	var parentID uint64 = 0

	req := &dto.UploadFileByHashRequest{
		UserId:   user.ID,
		ParentId: parentID,
		FileName: "new_file.txt",
		Hash:     "new_file_hash",
		IsDir:    false,
	}

	resp, err := service.FastUpload(nil, req)
	if err != nil {
		t.Fatalf("FastUpload failed: %v", err)
	}

	if resp.Instant {
		t.Fatal("FastUpload should return Instant=false when file does not exist")
	}
	if !resp.NeedUpload {
		t.Fatal("FastUpload should return need_upload=true when hash is not found")
	}
	if resp.Reason != "hash_not_found" {
		t.Fatalf("unexpected reason: %s", resp.Reason)
	}
}

// FastUpload - ?
func TestFastUploadSizeMismatchFallback(t *testing.T) {
	cleanFileObjectTables(t)

	user := createFileObjectTestUser(t, "fast_upload_size_mismatch")
	fileObj := &model.FileObject{
		Hash:       "size_mismatch_hash",
		BucketName: config.AppConfig.BucketName,
		ObjectName: "files/size_mismatch/hash",
		Size:       64,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}
	_, err := storage.Minio.Client.PutObject(
		context.Background(),
		fileObj.BucketName,
		fileObj.ObjectName,
		bytes.NewReader([]byte("size-mismatch-data")),
		int64(len("size-mismatch-data")),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("put object failed: %v", err)
	}

	req := &dto.UploadFileByHashRequest{
		UserId:   user.ID,
		ParentId: 0,
		FileName: "size_mismatch.txt",
		Size:     1024,
		Hash:     fileObj.Hash,
		IsDir:    false,
	}
	resp, err := service.FastUpload(nil, req)
	if err != nil {
		t.Fatalf("FastUpload failed: %v", err)
	}
	if resp.Instant {
		t.Fatal("FastUpload should return Instant=false when file size mismatch")
	}
	if !resp.NeedUpload {
		t.Fatal("FastUpload should return need_upload=true when file size mismatch")
	}
	if resp.Reason != "size_mismatch" {
		t.Fatalf("unexpected reason: %s", resp.Reason)
	}
}

// FastUpload - ?
func TestFastUploadObjectMissingFallback(t *testing.T) {
	cleanFileObjectTables(t)

	user := createFileObjectTestUser(t, "fast_upload_missing_obj")
	fileObj := &model.FileObject{
		Hash:       "missing_object_hash",
		BucketName: config.AppConfig.BucketName,
		ObjectName: "files/stale/missing_object_hash",
		Size:       12,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	req := &dto.UploadFileByHashRequest{
		UserId:   user.ID,
		ParentId: 0,
		FileName: "missing.txt",
		Hash:     fileObj.Hash,
		IsDir:    false,
	}
	resp, err := service.FastUpload(nil, req)
	if err != nil {
		t.Fatalf("FastUpload failed: %v", err)
	}
	if resp.Instant {
		t.Fatal("FastUpload should return Instant=false when object is missing in storage")
	}
	if !resp.NeedUpload {
		t.Fatal("FastUpload should return need_upload=true when object is missing in storage")
	}
	if resp.Reason != "object_missing" {
		t.Fatalf("unexpected reason: %s", resp.Reason)
	}
	var count int64
	if err := repo.Db.Model(&model.UserFile{}).Where("user_id = ? AND name = ?", user.ID, "missing.txt").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unexpected user file created: %d", count)
	}
}

// CreateUploadSession
func TestCreateUploadSession(t *testing.T) {
	cleanFileObjectTables(t)

	user := createFileObjectTestUser(t, "upload_session_user")

	req := dto.MultipartInitRequest{
		UserId:      user.ID,
		Hash:        "upload_session_hash",
		FileName:    "upload_file.txt",
		Size:        1024000,
		ChunkSize:   102400,
		TotalChunks: 10,
	}

	if err := service.CreateUploadSession(req); err != nil {
		t.Fatalf("CreateUploadSession failed: %v", err)
	}

	session, err := service.GetUploadSessionByHash(user.ID, req.Hash)
	if err != nil {
		t.Fatalf("failed to find upload session: %v", err)
	}
	if session.FileHash != req.Hash {
		t.Fatalf("expect hash %s, got %s", req.Hash, session.FileHash)
	}
}

// CompleteFile - hash
func TestCompleteFileRepairStaleObject(t *testing.T) {
	cleanFileObjectTables(t)

	user := createFileObjectTestUser(t, "repair_stale_obj")
	emptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	fileObj := &model.FileObject{
		Hash:       emptyHash,
		BucketName: config.AppConfig.BucketName,
		ObjectName: "files/stale/" + emptyHash,
		Size:       1,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	initReq := dto.MultipartInitRequest{
		UserId:      user.ID,
		FileName:    "repair.txt",
		Size:        0,
		Hash:        fileObj.Hash,
		ChunkSize:   1,
		TotalChunks: 0,
	}
	if err := service.CreateUploadSession(initReq); err != nil {
		t.Fatal(err)
	}

	completeReq := dto.MultipartCompleteRequest{
		FileHash:    fileObj.Hash,
		FileName:    "repair.txt",
		FileSize:    0,
		TotalChunks: 0,
		ParentId:    0,
		IsDir:       false,
	}
	if err := service.CompleteFile(context.Background(), completeReq, user.UserName); err != nil {
		t.Fatalf("CompleteFile failed: %v", err)
	}

	var userFile model.UserFile
	if err := repo.Db.Where("user_id = ? AND name = ?", user.ID, "repair.txt").First(&userFile).Error; err != nil {
		t.Fatalf("user file not created: %v", err)
	}
	if userFile.ObjectID == nil || *userFile.ObjectID != fileObj.ID {
		t.Fatalf("expected object_id=%d, got %+v", fileObj.ID, userFile.ObjectID)
	}

	object, _, err := storage.Default.GetObject(context.Background(), config.AppConfig.BucketName, fileObj.ObjectName)
	if err != nil {
		t.Fatalf("repaired object missing: %v", err)
	}
	_ = object.Close()
	_ = storage.Minio.Client.RemoveObject(context.Background(), config.AppConfig.BucketName, fileObj.ObjectName, minio.RemoveObjectOptions{})
}

// FindObjectIdByName
func TestFindObjectIdByName(t *testing.T) {
	cleanFileObjectTables(t)

	fileObj := &model.FileObject{
		Hash:       "find_by_name_hash",
		BucketName: "test-bucket",
		ObjectName: "test_object_find_by_name",
		Size:       131072,
		RefCount:   1,
	}

	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}

	// objectNameID
	id, err := service.FindObjectIdByName(fileObj.ObjectName)
	if err != nil {
		t.Fatalf("FindObjectIdByName failed: %v", err)
	}

	if id != fileObj.ID {
		t.Fatalf("expect ID %d, got %d", fileObj.ID, id)
	}
}
