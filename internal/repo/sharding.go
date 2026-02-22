package repo

import (
	"CloudVault/config"
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log"
	"sync"
	"time"
)

// ShardingConfig 分库分表配置
type ShardingConfig struct {
	// 分片数量
	ShardCount int

	// 是否启用分片
	Enabled bool

	// 分片
	ShardKey string
}

// ShardingManager 分库分表管理
type ShardingManager struct {
	config    ShardingConfig
	databases map[uint64]*gorm.DB // 按用户ID分库
	mu        sync.RWMutex
}

var globalShardingManager *ShardingManager
var shardingOnce sync.Once

// InitShardingManager 初始化分库分表管理器
func InitShardingManager(shardCount int, enabled bool) {
	shardingOnce.Do(func() {
		globalShardingManager = &ShardingManager{
			config: ShardingConfig{
				ShardCount: shardCount,
				Enabled:    enabled,
				ShardKey:   "user_id",
			},
			databases: make(map[uint64]*gorm.DB),
		}

		if enabled {
			log.Printf("Sharding manager initialized with %d shards", shardCount)
		} else {
			log.Println("Sharding is disabled")
		}
	})
}

// GetShardingManager 获取分库分表管理
func GetShardingManager() *ShardingManager {
	if globalShardingManager == nil {
		InitShardingManager(4, false) // 默认4个分片，默认不启
}
	return globalShardingManager
}

// GetShardDB 获取分片数据库连
func (sm *ShardingManager) GetShardDB(userID uint64) *gorm.DB {
	if !sm.config.Enabled {
		return Db // 如果未启用分片，返回默认数据
}

	// 计算分片索引
	shardIndex := userID % uint64(sm.config.ShardCount)

	// 检查缓
	sm.mu.RLock()
	if db, ok := sm.databases[shardIndex]; ok {
		sm.mu.RUnlock()
		return db
	}
	sm.mu.RUnlock()

	// 创建新的数据库连
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 再次检查缓存（双重检查）
	if db, ok := sm.databases[shardIndex]; ok {
		return db
	}

	// 创建数据库连
	db, err := sm.createShardDB(shardIndex)
	if err != nil {
		log.Printf("Failed to create shard DB for user %d: %v", userID, err)
		return Db // 返回默认数据
}

	sm.databases[shardIndex] = db
	return db
}

// createShardDB 创建分片数据库连
func (sm *ShardingManager) createShardDB(shardIndex uint64) (*gorm.DB, error) {
	// 构建分片数据库名
	shardDBName := fmt.Sprintf("%s_shard_%d", config.AppConfig.DBName, shardIndex)

	// 构建DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		config.AppConfig.DBUser,
		config.AppConfig.DBPass,
		config.AppConfig.DBHost,
		config.AppConfig.DBPort,
		shardDBName,
	)

	// 创建数据库连
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to shard database %s: %v", shardDBName, err)
	}

	// 配置连接
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %v", err)
	}

	// 设置最大空闲连接数
	sqlDB.SetMaxIdleConns(10)

	// 设置最大打开连接
	sqlDB.SetMaxOpenConns(100)

	// 设置连接最大可复用时间
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Printf("Successfully connected to shard database %s", shardDBName)

	return db, nil
}

// GetTableName 获取分片表名
func (sm *ShardingManager) GetTableName(baseTableName string, userID uint64) string {
	if !sm.config.Enabled {
		return baseTableName
	}

	// 计算分片索引
	shardIndex := userID % uint64(sm.config.ShardCount)

	// 返回分片表名
	return fmt.Sprintf("%s_%d", baseTableName, shardIndex)
}

// CloseShardConnections 关闭所有分片数据库连接
func (sm *ShardingManager) CloseShardConnections() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var lastErr error
	for shardIndex, db := range sm.databases {
		sqlDB, err := db.DB()
		if err != nil {
			log.Printf("Failed to get database instance for shard %d: %v", shardIndex, err)
			continue
		}

		if err := sqlDB.Close(); err != nil {
			log.Printf("Failed to close database connection for shard %d: %v", shardIndex, err)
			lastErr = err
		}
	}

	sm.databases = make(map[uint64]*gorm.DB)
	return lastErr
}




