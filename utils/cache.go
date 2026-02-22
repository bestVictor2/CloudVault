package utils

import (
	"CloudVault/internal/repo"
	"CloudVault/model"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

type RedisCache struct {
	client *redis.Client
}

// NewRedisCache creates a Redis cache client.
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{
		client: client,
	}
}

// Get reads a cached value.
func (c *RedisCache) Get(ctx context.Context, key string, dest interface{}) error {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return err
	}
	// 关于 Unmarshal 函数 用于将 json 格式反序列化存入 dest 中 用在 gin 的 shoubindjson 中
	return json.Unmarshal([]byte(val), dest)
}

// Set writes a cached value.
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	// Marshal 函数是转化为json类型切片
	if err != nil {
		return err
	}

	return c.client.Set(ctx, key, string(data), expiration).Err()
}

// Delete removes a cache entry.
func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

// DeleteByPattern deletes cache entries by pattern.
func (c *RedisCache) DeleteByPattern(ctx context.Context, pattern string) error {
	var cursor uint64
	// cursor 的默认值默认为0
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

// Exists checks whether a cache key exists.
func (c *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	count, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

type CacheManager struct {
	cache Cache
}

var globalCacheManager *CacheManager
var cacheManagerOnce sync.Once

// InitCacheManager initializes the cache manager.
func InitCacheManager() {
	cacheManagerOnce.Do(func() { // 单一用例模式
		globalCacheManager = &CacheManager{
			cache: NewRedisCache(repo.Redis),
		}
	})
}

// GetCacheManager returns the cache manager.
func GetCacheManager() *CacheManager {
	if globalCacheManager == nil {
		InitCacheManager()
	}
	return globalCacheManager
}

// BuildCacheKey builds a cache key.
func BuildCacheKey(prefix string, params ...interface{}) string {
	key := prefix
	for _, param := range params {
		key += fmt.Sprintf(":%v", param)
	}
	return key
}

const (
	CacheKeyUserFileList = "user:file:list"
	CacheKeyUserInfo     = "user:info"
	CacheKeyFileObject   = "file:object"
)

type FileListCache struct {
	Files []model.UserFile `json:"files"`
	Total int64            `json:"total"`
}

// GetUserFileListFromCache reads cached file list.
func GetUserFileListFromCache(
	ctx context.Context,
	userId uint64,
	parentId uint64,
	page int,
	pageSize int,
	orderBy string,
	orderDesc bool,
) (*FileListCache, bool) {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyUserFileList, userId, parentId, page, pageSize, orderBy, orderDesc)

	var result FileListCache
	if err := manager.cache.Get(ctx, key, &result); err != nil {
		return nil, false
	}
	return &result, true
}

// SetUserFileListToCache writes cached file list.
func SetUserFileListToCache(
	ctx context.Context,
	userId uint64,
	parentId uint64,
	page int,
	pageSize int,
	orderBy string,
	orderDesc bool,
	data *FileListCache,
	expiration time.Duration,
) error {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyUserFileList, userId, parentId, page, pageSize, orderBy, orderDesc)
	return manager.cache.Set(ctx, key, data, expiration)
}

// InvalidateUserFileListCache clears cached file lists.
func InvalidateUserFileListCache(ctx context.Context, userId uint64, parentId uint64) error {
	manager := GetCacheManager()
	keyPattern := BuildCacheKey(CacheKeyUserFileList, userId, parentId) + ":*"
	cache, ok := manager.cache.(*RedisCache)
	if !ok {
		return manager.cache.Delete(ctx, keyPattern)
	}
	return cache.DeleteByPattern(ctx, keyPattern)
}

// GetUserInfoFromCache reads cached user info.
func GetUserInfoFromCache(ctx context.Context, userId uint64) (*model.User, bool) {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyUserInfo, userId)

	var result model.User
	if err := manager.cache.Get(ctx, key, &result); err != nil {
		return nil, false
	}

	return &result, true
}

// SetUserInfoToCache writes cached user info.
func SetUserInfoToCache(ctx context.Context, userId uint64, data *model.User, expiration time.Duration) error {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyUserInfo, userId)
	return manager.cache.Set(ctx, key, data, expiration)
}

// InvalidateUserInfoCache clears cached user info.
func InvalidateUserInfoCache(ctx context.Context, userId uint64) error {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyUserInfo, userId)
	return manager.cache.Delete(ctx, key)
}

// GetFileObjectFromCache reads cached file object.
func GetFileObjectFromCache(ctx context.Context, objectId uint64) (*model.FileObject, bool) {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyFileObject, objectId)

	var result model.FileObject
	if err := manager.cache.Get(ctx, key, &result); err != nil {
		return nil, false
	}

	return &result, true
}

// SetFileObjectToCache writes cached file object.
func SetFileObjectToCache(ctx context.Context, objectId uint64, data *model.FileObject, expiration time.Duration) error {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyFileObject, objectId)
	return manager.cache.Set(ctx, key, data, expiration)
}

// InvalidateFileObjectCache clears cached file object.
func InvalidateFileObjectCache(ctx context.Context, objectId uint64) error {
	manager := GetCacheManager()
	key := BuildCacheKey(CacheKeyFileObject, objectId)
	return manager.cache.Delete(ctx, key)
}
