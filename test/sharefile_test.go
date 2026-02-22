package test

import (
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/model"
	"golang.org/x/net/context"
	"testing"
	"time"
)

// cleanTables clears test tables.
func cleanTables(t *testing.T) { // 清理函数
	db := repo.Db

	// 临时禁用外键检
	db.Exec("SET FOREIGN_KEY_CHECKS = 0")

	// 按照外键依赖关系的顺序清理表数据
	tables := []string{
		"file_share",
		"file_chunk",
		"upload_session",
		"user_file",
		"file_object",
		"user_db",
	}

	for _, table := range tables {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s failed: %v", table, err)
		}
	}

	// 重新启用外键检
	db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

// prepareUserAndFile seeds user and file for tests.
func prepareUserAndFile(t *testing.T) (userID uint64, fileID uint64) {
	user := model.User{
		UserName: "test_user",
		Email:    "test@test.com",
		IsActive: true,
	}
	if err := repo.Db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	file := model.UserFile{
		UserID:    user.ID,
		Name:      "test.txt",
		IsDir:     false,
		IsDeleted: false,
	}
	if err := repo.Db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}

	return user.ID, file.ID
}

// TestCreateShare tests share creation.
func TestCreateShare(t *testing.T) {
	cleanTables(t)
	userID, fileID := prepareUserAndFile(t)

	share, err := service.CreateShare(userID, fileID, 1, true)
	if err != nil {
		t.Fatalf("CreateShare failed: %v", err)
	}

	if share.ShareID == "" {
		t.Fatal("shareID is empty")
	}

	key := "share:" + share.ShareID
	exists, err := repo.Redis.Exists(context.Background(), key).Result()
	if err != nil {
		t.Fatal(err)
	}

	if exists != 1 {
		t.Fatal("redis key not exists")
	}
}

// TestCheckShare tests share validation.
func TestCheckShare(t *testing.T) {
	cleanTables(t)
	userID, fileID := prepareUserAndFile(t)

	share, err := service.CreateShare(userID, fileID, 1, true)
	if err != nil {
		t.Fatal(err)
	}

	// 正确提取
	_, err = service.CheckShare(share.ShareID, share.ExtractCode)
	if err != nil {
		t.Fatalf("CheckShare failed: %v", err)
	}

	// 错误提取
	_, err = service.CheckShare(share.ShareID, "WRONG")
	if err == nil {
		t.Fatal("expect extract code error")
	}
}

// TestShareExpiredListener tests share expiry handler.
func TestShareExpiredListener(t *testing.T) {
	cleanTables(t)
	userID, fileID := prepareUserAndFile(t)

	share, err := service.CreateShare(userID, fileID, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.EnableKeyspaceNotifications(context.Background()); err != nil {
		t.Skipf("skip: redis keyspace notifications not enabled: %v", err)
	}
	//go repo.ListenRedisExpired(context.Background(), repo.Redis)
	// 手动设置一个极 TTL

	key := "share:" + share.ShareID
	//log.Println("[test] expire key =", key)
	//log.Println("[test] redis db =", repo.Redis.Options().DB)
	if err := repo.Redis.Expire(context.Background(), key, 3*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	//ttl, _ := repo.Redis.TTL(context.Background(), key).Result()
	//log.Println("[test] ttl =", ttl)

	// 等待 Redis 过期事件
	time.Sleep(5 * time.Second)

	var status int
	err = repo.Db.
		Model(&model.FileShare{}).
		Where("share_id = ?", share.ShareID).
		Select("status").
		Scan(&status).Error

	if err != nil {
		t.Fatal(err)
	}

	if status != 1 {
		t.Fatalf("expect status=1, got %d", status)
	}
}
