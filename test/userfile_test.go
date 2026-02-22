package test

import (
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/model"
	"fmt"
	"testing"
)

// 清理测试数据
func cleanUserFileTables(t *testing.T) {
	// 临时禁用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")

	// 按照外键依赖关系的顺序清理表数据
	tables := []string{"file_share", "file_chunk", "upload_session", "user_file", "file_object", "user_db"}
	for _, table := range tables {
		if err := repo.Db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s failed: %v", table, err)
		}
	}

	// 重新启用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

// 创建测试用户
func createTestUser(t *testing.T) *model.User {
	user := &model.User{
		UserName: "test_user_file",
		Password: "123456",
		Email:    "test_file@test.com",
		IsActive: true,
	}
	if err := service.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	return user
}

// 创建测试文件对象
func createTestFileObject(t *testing.T, userID uint64) *model.FileObject {
	fileObj := &model.FileObject{
		UserID:     userID,
		Hash:       "test_hash_123",
		BucketName: "test-bucket",
		ObjectName: "test_object_name",
		Size:       1024,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		t.Fatal(err)
	}
	return fileObj
}

// 测试CreateUserFileEntry - 创建文件
func TestCreateUserFileEntry(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)
	fileObj := createTestFileObject(t, user.ID)

	userFile := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "test_file.txt",
		IsDir:    false,
		ObjectID: &fileObj.ID,
		Size:     1024,
	}

	if err := service.CreateUserFileEntry(userFile); err != nil {
		t.Fatalf("CreateUserFileEntry failed: %v", err)
	}

	if userFile.ID == 0 {
		t.Fatal("file ID should not be zero after create")
	}
}

// 测试CreateUserFileEntry - 创建文件
func TestCreateUserDirEntry(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)

	dir := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "test_folder",
		IsDir:    true,
	}

	if err := service.CreateUserFileEntry(dir); err != nil {
		t.Fatalf("CreateUserFileEntry (dir) failed: %v", err)
	}

	if dir.ID == 0 {
		t.Fatal("dir ID should not be zero after create")
	}
}

// 测试CreateUserFileEntry - 创建子文件夹
func TestCreateUserSubDirEntry(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)

	// 创建父文件夹
	parentDir := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "parent_folder",
		IsDir:    true,
	}
	if err := service.CreateUserFileEntry(parentDir); err != nil {
		t.Fatal(err)
	}

	// 创建子文件夹
	subDir := &model.UserFile{
		UserID:   user.ID,
		ParentID: &parentDir.ID,
		Name:     "sub_folder",
		IsDir:    true,
	}

	if err := service.CreateUserFileEntry(subDir); err != nil {
		t.Fatalf("CreateUserFileEntry (subdir) failed: %v", err)
	}

	if subDir.ID == 0 {
		t.Fatal("subdir ID should not be zero after create")
	}
}

// 测试CreateUserFileEntry - 创建文件时没有ObjectID应该失败
func TestCreateUserFileWithoutObjectID(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)

	userFile := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "test_file.txt",
		IsDir:    false,
		ObjectID: nil,
		Size:     1024,
	}

	if err := service.CreateUserFileEntry(userFile); err == nil {
		t.Fatal("CreateUserFileEntry should fail when ObjectID is nil")
	}
}

// 测试MoveToRecycle
func TestMoveToRecycle(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)
	fileObj := createTestFileObject(t, user.ID)

	userFile := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "test_file.txt",
		IsDir:    false,
		ObjectID: &fileObj.ID,
		Size:     1024,
	}

	if err := service.CreateUserFileEntry(userFile); err != nil {
		t.Fatal(err)
	}

	// 移入回收
	if err := service.MoveToRecycle(user.ID, userFile.ID); err != nil {
		t.Fatalf("MoveToRecycle failed: %v", err)
	}

	// 验证文件已被标记为删
	file, err := service.GetDeletedFile(uint(user.ID), uint(userFile.ID))
	if err != nil {
		t.Fatalf("GetDeletedFile failed: %v", err)
	}

	if !file.IsDeleted {
		t.Fatal("file should be marked as deleted")
	}
}

// 测试ListRecycleFiles
func TestListRecycleFiles(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)
	fileObj := createTestFileObject(t, user.ID)

	// 创建多个文件并移入回收站
	for i := 0; i < 3; i++ {
		userFile := &model.UserFile{
			UserID:   user.ID,
			ParentID: nil,
			Name:     fmt.Sprintf("test_file_%d.txt", i),
			IsDir:    false,
			ObjectID: &fileObj.ID,
			Size:     1024,
		}
		if err := service.CreateUserFileEntry(userFile); err != nil {
			t.Fatal(err)
		}
		if err := service.MoveToRecycle(user.ID, userFile.ID); err != nil {
			t.Fatal(err)
		}
	}

	// 获取回收站文件列
	files, err := service.ListRecycleFiles(uint(user.ID))
	if err != nil {
		t.Fatalf("ListRecycleFiles failed: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expect 3 files in recycle, got %d", len(files))
	}
}

// 测试RestoreFile
func TestRestoreFile(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)
	fileObj := createTestFileObject(t, user.ID)

	userFile := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "test_file.txt",
		IsDir:    false,
		ObjectID: &fileObj.ID,
		Size:     1024,
	}

	if err := service.CreateUserFileEntry(userFile); err != nil {
		t.Fatal(err)
	}

	// 移入回收
	if err := service.MoveToRecycle(user.ID, userFile.ID); err != nil {
		t.Fatal(err)
	}

	// 恢复文件
	if err := service.RestoreFile(uint(user.ID), uint(userFile.ID)); err != nil {
		t.Fatalf("RestoreFile failed: %v", err)
	}

	// 验证文件已恢
	_, err := service.GetDeletedFile(uint(user.ID), uint(userFile.ID))
	if err == nil {
		t.Fatal("GetDeletedFile should return error after restore")
	}
}

// 测试CheckFileOwner
func TestCheckFileOwner(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)
	fileObj := createTestFileObject(t, user.ID)

	userFile := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "test_file.txt",
		IsDir:    false,
		ObjectID: &fileObj.ID,
		Size:     1024,
	}

	if err := service.CreateUserFileEntry(userFile); err != nil {
		t.Fatal(err)
	}

	// 验证文件所有
	if !service.CheckFileOwner(user.ID, userFile.ID) {
		t.Fatal("CheckFileOwner should return true for owner")
	}

	// 验证非文件所有
	if service.CheckFileOwner(user.ID+1, userFile.ID) {
		t.Fatal("CheckFileOwner should return false for non-owner")
	}
}

// 测试GetDeletedFile
func TestGetDeletedFile(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)
	fileObj := createTestFileObject(t, user.ID)

	userFile := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "test_file.txt",
		IsDir:    false,
		ObjectID: &fileObj.ID,
		Size:     1024,
	}

	if err := service.CreateUserFileEntry(userFile); err != nil {
		t.Fatal(err)
	}

	// 移入回收
	if err := service.MoveToRecycle(user.ID, userFile.ID); err != nil {
		t.Fatal(err)
	}

	// 获取已删除文
	file, err := service.GetDeletedFile(uint(user.ID), uint(userFile.ID))
	if err != nil {
		t.Fatalf("GetDeletedFile failed: %v", err)
	}

	if file.ID != userFile.ID {
		t.Fatalf("expect file ID %d, got %d", userFile.ID, file.ID)
	}

	if !file.IsDeleted {
		t.Fatal("file should be marked as deleted")
	}
}



