package main

import (
	"Go_Pan/config"
	"Go_Pan/internal/repo"
	"Go_Pan/internal/storage"
	"Go_Pan/internal/worker"
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

	log.Println("workers started: download + activity")

	errCh := make(chan error, 2)
	go func() {
		errCh <- worker.RunDownloadWorker(ctx)
	}()
	go func() {
		errCh <- worker.RunActivityWorker(ctx)
	}()

	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil {
			log.Fatalf("worker stopped: %v", err)
		}
	}
}
