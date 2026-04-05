package main

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/internal/storage"
	"CloudVault/router"
	"context"
	"log"
)

// main initializes services and starts the HTTP server.
func main() {
	config.InitConfig()
	repo.InitMysql()
	repo.InitRedis()
	storage.InitMinio()

	ctx := context.Background()
	service.StartUploadSessionWatchdog(
		ctx,
		config.AppConfig.UploadWatchdogInterval,
		config.AppConfig.UploadSessionTTL,
		config.AppConfig.UploadWatchdogBatch,
	)
	service.StartFileObjectCleanupWatchdog(
		ctx,
		config.AppConfig.FileObjectCleanupInterval,
		config.AppConfig.FileObjectCleanupBatch,
	)
	service.StartFileObjectRefCountSyncWatchdog(
		ctx,
		config.AppConfig.FileObjectRefSyncInterval,
		config.AppConfig.FileObjectRefSyncBatch,
	)
	if err := repo.EnableKeyspaceNotifications(ctx); err != nil {
		log.Printf("enable redis keyspace notifications failed: %v", err)
	} else {
		ready := make(chan struct{})
		go repo.ListenRedisExpired(ctx, repo.Redis, ready)
		<-ready
	}

	router := router.InitRouter()

	router.Run(":8000")
}
