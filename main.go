package main

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
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



