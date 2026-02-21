package repo

import (
	"Go_Pan/config"
	"Go_Pan/model"
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"log"
	"time"
)

var Db *gorm.DB

// autoMigrateAll migrates all database models.
func autoMigrateAll(db *gorm.DB) {
	db.AutoMigrate(&model.User{})
	db.AutoMigrate(&model.FileObject{})
	db.AutoMigrate(&model.UserFile{})
	db.AutoMigrate(&model.FileChunk{})
	db.AutoMigrate(&model.UploadSession{})
	db.AutoMigrate(&model.FileShare{})
	db.AutoMigrate(&model.DownloadTask{})
	db.AutoMigrate(&model.UserActivityDaily{})
	db.AutoMigrate(&model.UserFavorite{})
	db.AutoMigrate(&model.UserRecent{})
	db.AutoMigrate(&model.ShareAccessLog{})
}

// InitMysql initializes the main MySQL connection.
func InitMysql() {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		config.AppConfig.DBUser,
		config.AppConfig.DBPass,
		config.AppConfig.DBHost,
		config.AppConfig.DBPort,
		config.AppConfig.DBName,
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("init mysql fail", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("get sql db fail", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	autoMigrateAll(db)
	log.Println("init mysql success")
	Db = db
}

// InitMysqlTest initializes the test MySQL connection.
func InitMysqlTest() {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		config.AppConfig.DBUser,
		config.AppConfig.DBPass,
		config.AppConfig.DBHost,
		config.AppConfig.DBPort,
		config.AppConfig.DBNameTest,
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("init mysql fail", err)
	}

	autoMigrateAll(db)

	log.Println("init mysql success")
	Db = db
}
