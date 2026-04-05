package repo

import (
	"CloudVault/config"
	"CloudVault/model"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	gormMysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var Db *gorm.DB

// autoMigrateAll migrates all database models.
func autoMigrateAll(db *gorm.DB) {
	db.AutoMigrate(&model.User{})
	db.AutoMigrate(&model.FileObject{})
	migrateFileObjectSchema(db)
	db.AutoMigrate(&model.UserFile{})
	migrateUserFileIndexes(db)
	db.AutoMigrate(&model.FileChunk{})
	db.AutoMigrate(&model.UploadSession{})
	db.AutoMigrate(&model.FileShare{})
	db.AutoMigrate(&model.DownloadTask{})
	db.AutoMigrate(&model.UserActivityDaily{})
	db.AutoMigrate(&model.UserFavorite{})
	db.AutoMigrate(&model.UserRecent{})
	db.AutoMigrate(&model.ShareAccessLog{})
}

// migrateFileObjectSchema drops legacy user ownership from file_object.
func migrateFileObjectSchema(db *gorm.DB) {
	if db == nil {
		return
	}
	migrator := db.Migrator()
	const tableName = "file_object"
	const columnName = "user_id"
	if !migrator.HasColumn(&model.FileObject{}, columnName) {
		return
	}

	type constraintRow struct {
		Name string `gorm:"column:CONSTRAINT_NAME"`
	}
	var constraints []constraintRow
	if err := db.Raw(
		`SELECT CONSTRAINT_NAME
FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME = ?
  AND COLUMN_NAME = ?
  AND REFERENCED_TABLE_NAME IS NOT NULL`,
		tableName,
		columnName,
	).Scan(&constraints).Error; err != nil {
		log.Printf("list file_object foreign keys failed: %v", err)
	}
	for _, constraint := range constraints {
		name := strings.TrimSpace(constraint.Name)
		if name == "" {
			continue
		}
		if err := db.Exec(
			"ALTER TABLE " + quoteMySQLIdentifier(tableName) + " DROP FOREIGN KEY " + quoteMySQLIdentifier(name),
		).Error; err != nil {
			log.Printf("drop file_object foreign key %s failed: %v", name, err)
		}
	}

	type indexRow struct {
		Name string `gorm:"column:INDEX_NAME"`
	}
	var indexes []indexRow
	if err := db.Raw(
		`SELECT DISTINCT INDEX_NAME
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME = ?
  AND COLUMN_NAME = ?
  AND INDEX_NAME <> 'PRIMARY'`,
		tableName,
		columnName,
	).Scan(&indexes).Error; err != nil {
		log.Printf("list file_object indexes failed: %v", err)
	}
	for _, index := range indexes {
		name := strings.TrimSpace(index.Name)
		if name == "" {
			continue
		}
		if err := db.Exec(
			"ALTER TABLE " + quoteMySQLIdentifier(tableName) + " DROP INDEX " + quoteMySQLIdentifier(name),
		).Error; err != nil {
			log.Printf("drop file_object index %s failed: %v", name, err)
		}
	}

	if err := migrator.DropColumn(&model.FileObject{}, columnName); err != nil {
		log.Printf("drop file_object.%s failed: %v", columnName, err)
	}
}

// migrateUserFileIndexes keeps user_file uniqueness aligned with active/deleted state.
func migrateUserFileIndexes(db *gorm.DB) {
	if db == nil {
		return
	}
	migrator := db.Migrator()
	const oldIndex = "uk_user_parent_name"
	const newIndex = "uk_user_parent_name_active"

	if migrator.HasIndex(&model.UserFile{}, oldIndex) {
		if err := migrator.DropIndex(&model.UserFile{}, oldIndex); err != nil {
			log.Printf("drop index %s failed: %v", oldIndex, err)
		}
	}
	if !migrator.HasIndex(&model.UserFile{}, newIndex) {
		if err := migrator.CreateIndex(&model.UserFile{}, newIndex); err != nil {
			log.Printf("create index %s failed: %v", newIndex, err)
		}
	}
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
	db, err := gorm.Open(gormMysql.Open(dsn), &gorm.Config{})
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
	db, err := gorm.Open(gormMysql.Open(dsn), &gorm.Config{})
	if err != nil && isUnknownDatabaseError(err) {
		if createErr := ensureMySQLDatabase(config.AppConfig.DBNameTest); createErr != nil {
			log.Fatal("create test mysql database fail", createErr)
		}
		db, err = gorm.Open(gormMysql.Open(dsn), &gorm.Config{})
	}
	if err != nil {
		log.Fatal("init mysql fail", err)
	}

	autoMigrateAll(db)

	log.Println("init mysql success")
	Db = db
}

func isUnknownDatabaseError(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1049
	}
	return strings.Contains(strings.ToLower(err.Error()), "unknown database")
}

func ensureMySQLDatabase(dbName string) error {
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return errors.New("empty database name")
	}

	serverDSN := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&loc=Local",
		config.AppConfig.DBUser,
		config.AppConfig.DBPass,
		config.AppConfig.DBHost,
		config.AppConfig.DBPort,
	)

	serverDB, err := sql.Open("mysql", serverDSN)
	if err != nil {
		return err
	}
	defer serverDB.Close()

	if err = serverDB.Ping(); err != nil {
		return err
	}

	_, err = serverDB.Exec(
		"CREATE DATABASE IF NOT EXISTS " + quoteMySQLIdentifier(dbName) + " CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci",
	)
	return err
}

func quoteMySQLIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
