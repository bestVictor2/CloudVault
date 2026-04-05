package test

import (
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/model"
	"fmt"
	"testing"
)

// 娓呯悊娴嬭瘯鏁版嵁
func cleanUserFileTables(t *testing.T) {
	// 涓存椂绂佺敤澶栭敭妫€
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")

	// 鎸夌収澶栭敭渚濊禆鍏崇郴鐨勯『搴忔竻鐞嗚〃鏁版嵁
	tables := []string{"file_share", "file_chunk", "upload_session", "user_file", "file_object", "user_db"}
	for _, table := range tables {
		if err := repo.Db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s failed: %v", table, err)
		}
	}

	// 閲嶆柊鍚敤澶栭敭妫€
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

// 鍒涘缓娴嬭瘯鐢ㄦ埛
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

// 鍒涘缓娴嬭瘯鏂囦欢瀵硅薄
func createTestFileObject(t *testing.T, userID uint64) *model.FileObject {
	fileObj := &model.FileObject{
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

// 娴嬭瘯CreateUserFileEntry - 鍒涘缓鏂囦欢
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

// 娴嬭瘯CreateUserFileEntry - 鍒涘缓鏂囦欢
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

// 娴嬭瘯CreateUserFileEntry - 鍒涘缓瀛愭枃浠跺す
func TestCreateUserSubDirEntry(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)

	// 鍒涘缓鐖舵枃浠跺す
	parentDir := &model.UserFile{
		UserID:   user.ID,
		ParentID: nil,
		Name:     "parent_folder",
		IsDir:    true,
	}
	if err := service.CreateUserFileEntry(parentDir); err != nil {
		t.Fatal(err)
	}

	// 鍒涘缓瀛愭枃浠跺す
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

// 娴嬭瘯CreateUserFileEntry - 鍒涘缓鏂囦欢鏃舵病鏈塐bjectID搴旇澶辫触
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

// 娴嬭瘯MoveToRecycle
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

	// 绉诲叆鍥炴敹
	if err := service.MoveToRecycle(user.ID, userFile.ID); err != nil {
		t.Fatalf("MoveToRecycle failed: %v", err)
	}

	// 楠岃瘉鏂囦欢宸茶鏍囪涓哄垹
	file, err := service.GetDeletedFile(uint(user.ID), uint(userFile.ID))
	if err != nil {
		t.Fatalf("GetDeletedFile failed: %v", err)
	}

	if !file.IsDeleted {
		t.Fatal("file should be marked as deleted")
	}
}

// 娴嬭瘯ListRecycleFiles
func TestListRecycleFiles(t *testing.T) {
	cleanUserFileTables(t)
	user := createTestUser(t)
	fileObj := createTestFileObject(t, user.ID)

	// 鍒涘缓澶氫釜鏂囦欢骞剁Щ鍏ュ洖鏀剁珯
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

	// 鑾峰彇鍥炴敹绔欐枃浠跺垪
	files, err := service.ListRecycleFiles(uint(user.ID))
	if err != nil {
		t.Fatalf("ListRecycleFiles failed: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expect 3 files in recycle, got %d", len(files))
	}
}

// 娴嬭瘯RestoreFile
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

	// 绉诲叆鍥炴敹
	if err := service.MoveToRecycle(user.ID, userFile.ID); err != nil {
		t.Fatal(err)
	}

	// 鎭㈠鏂囦欢
	if err := service.RestoreFile(uint(user.ID), uint(userFile.ID)); err != nil {
		t.Fatalf("RestoreFile failed: %v", err)
	}

	// 楠岃瘉鏂囦欢宸叉仮
	_, err := service.GetDeletedFile(uint(user.ID), uint(userFile.ID))
	if err == nil {
		t.Fatal("GetDeletedFile should return error after restore")
	}
}

// 娴嬭瘯CheckFileOwner
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

	// 楠岃瘉鏂囦欢鎵€鏈?
	if !service.CheckFileOwner(user.ID, userFile.ID) {
		t.Fatal("CheckFileOwner should return true for owner")
	}

	// 楠岃瘉闈炴枃浠舵墍鏈?
	if service.CheckFileOwner(user.ID+1, userFile.ID) {
		t.Fatal("CheckFileOwner should return false for non-owner")
	}
}

// 娴嬭瘯GetDeletedFile
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

	// 绉诲叆鍥炴敹
	if err := service.MoveToRecycle(user.ID, userFile.ID); err != nil {
		t.Fatal(err)
	}

	// 鑾峰彇宸插垹闄ゆ枃
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
