package test

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"log"
	"os"
	"testing"

	"github.com/minio/minio-go/v7"
	"golang.org/x/net/context"
)

// ensureTestBucket ensures the test bucket exists.
func ensureTestBucket() {
	ctx := context.Background()
	exists, err := storage.Minio.Client.BucketExists(ctx, storage.Minio.Bucket)
	if err != nil {
		panic(err)
	}
	if !exists {
		err = storage.Minio.Client.MakeBucket(ctx, storage.Minio.Bucket, minio.MakeBucketOptions{})
		if err != nil {
			panic(err)
		}
	}
}

// TestMain sets up the test environment.
func TestMain(m *testing.M) {
	_ = os.Setenv("DOWNLOAD_ALLOW_PRIVATE", "true")
	config.InitConfig()
	repo.InitMysqlTest()
	storage.InitMinio()
	repo.InitRedis()
	if err := repo.EnableKeyspaceNotifications(context.Background()); err != nil {
		log.Printf("[testmain] enable redis keyspace notifications failed: %v", err)
	}
	log.Println("[testmain] redis db =", repo.Redis.Options().DB)
	ready := make(chan struct{})
	go repo.ListenRedisExpired(context.Background(), repo.Redis, ready)
	<-ready

	ensureTestBucket()

	// 在测试开始前清理所有表的数
	cleanupAllTables()

	code := m.Run()
	os.Exit(code)
}

// cleanupAllTables 清理所有表的数据（不删除表结构
func cleanupAllTables() {
	// 临时禁用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")

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
		repo.Db.Exec("DELETE FROM " + table)
	}

	// 重新启用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")

	log.Println("[testmain] all tables cleaned")
}
