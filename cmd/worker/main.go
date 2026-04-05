package main

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/internal/storage"
	"CloudVault/internal/worker"
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	config.InitConfig()
	repo.InitMysql()
	repo.InitRedis()
	storage.InitMinio()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	log.Println("workers started: download + activity + merge")

	errCh := make(chan error, 3)
	go func() {
		errCh <- worker.RunDownloadWorker(ctx)
	}()
	go func() {
		errCh <- worker.RunActivityWorker(ctx)
	}()
	go func() {
		errCh <- worker.RunMergeWorker(ctx)
	}()

	for i := 0; i < 3; i++ {
		err := <-errCh
		if err != nil {
			log.Fatalf("worker stopped: %v", err)
		}
	}
}
