package service

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/model"
	"CloudVault/utils"
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	fileObjectRefCountKeyPrefix = "file_object:ref_count:"
	fileObjectRefDirtySetKey    = "file_object:ref_count:dirty"
)

var adjustRefCountScript = redis.NewScript(`
local key = KEYS[1]
local dirty = KEYS[2]
local delta = tonumber(ARGV[1])
local object_id = ARGV[2]

local cur = redis.call("GET", key)
if not cur then
	cur = 0
else
	cur = tonumber(cur)
end

local next = cur + delta
if next < 0 then
	next = 0
end

redis.call("SET", key, tostring(next))
redis.call("SADD", dirty, object_id)
return next
`)

func fileObjectRefCountKey(objectID uint64) string {
	return fileObjectRefCountKeyPrefix + strconv.FormatUint(objectID, 10)
}

func ensureRefCountCache(ctx context.Context, tx *gorm.DB, objectID uint64) (int64, error) {
	if repo.Redis == nil {
		return 0, fmt.Errorf("redis not initialized")
	}
	key := fileObjectRefCountKey(objectID)
	value, err := repo.Redis.Get(ctx, key).Result()
	if err == nil {
		out, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr == nil {
			if out < 0 {
				return 0, nil
			}
			return out, nil
		}
	}
	if err != nil && err != redis.Nil {
		return 0, err
	}

	db := tx
	if db == nil {
		db = repo.Db
	}
	var obj model.FileObject
	if err := db.Select("id", "ref_count").Where("id = ?", objectID).First(&obj).Error; err != nil {
		return 0, err
	}
	count := int64(obj.RefCount)
	if count < 0 {
		count = 0
	}
	_ = repo.Redis.SetNX(ctx, key, strconv.FormatInt(count, 10), 0).Err()
	return count, nil
}

func setRefCountCache(ctx context.Context, objectID uint64, count int64) {
	if repo.Redis == nil {
		return
	}
	if count < 0 {
		count = 0
	}
	_ = repo.Redis.Set(ctx, fileObjectRefCountKey(objectID), strconv.FormatInt(count, 10), 0).Err()
}

func adjustFileObjectRefCount(ctx context.Context, tx *gorm.DB, objectID uint64, delta int64) (int64, error) {
	if objectID == 0 {
		return 0, fmt.Errorf("invalid object id")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if repo.Redis == nil {
		return adjustFileObjectRefCountInDB(ctx, tx, objectID, delta)
	}
	if _, err := ensureRefCountCache(ctx, tx, objectID); err != nil {
		return 0, err
	}
	result, err := adjustRefCountScript.Run(
		ctx,
		repo.Redis,
		[]string{fileObjectRefCountKey(objectID), fileObjectRefDirtySetKey},
		delta,
		strconv.FormatUint(objectID, 10),
	).Result()
	if err != nil {
		return adjustFileObjectRefCountInDB(ctx, tx, objectID, delta)
	}
	switch v := result.(type) {
	case int64:
		if v < 0 {
			return 0, nil
		}
		return v, nil
	case string:
		out, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil {
			return 0, parseErr
		}
		if out < 0 {
			return 0, nil
		}
		return out, nil
	default:
		return 0, fmt.Errorf("unexpected redis reply type: %T", result)
	}
}

func adjustFileObjectRefCountInDB(ctx context.Context, tx *gorm.DB, objectID uint64, delta int64) (int64, error) {
	db := tx
	if db == nil {
		db = repo.Db
	}
	var (
		obj      model.FileObject
		newCount int64
	)
	err := db.Transaction(func(inner *gorm.DB) error {
		if err := inner.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", objectID).
			First(&obj).Error; err != nil {
			return err
		}
		newCount = int64(obj.RefCount) + delta
		if newCount < 0 {
			newCount = 0
		}
		return inner.Model(&model.FileObject{}).
			Where("id = ?", objectID).
			Update("ref_count", newCount).Error
	})
	if err != nil {
		return 0, err
	}
	setRefCountCache(ctx, objectID, newCount)
	return newCount, nil
}

// FlushFileObjectRefCounts flushes dirty ref_count cache entries back to MySQL in batches.
func FlushFileObjectRefCounts(ctx context.Context, batch int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if repo.Redis == nil {
		return 0, nil
	}
	if batch <= 0 {
		batch = 200
	}
	ids, err := repo.Redis.SPopN(ctx, fileObjectRefDirtySetKey, int64(batch)).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	synced := 0
	var firstErr error
	for _, raw := range ids {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		objectID, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil || objectID == 0 {
			continue
		}
		ok, syncErr := flushOneObjectRefCount(ctx, objectID)
		if syncErr != nil {
			if firstErr == nil {
				firstErr = syncErr
			}
			_ = repo.Redis.SAdd(ctx, fileObjectRefDirtySetKey, raw).Err()
			continue
		}
		if ok {
			synced++
		}
	}
	return synced, firstErr
}

func flushOneObjectRefCount(ctx context.Context, objectID uint64) (bool, error) {
	now := time.Now()
	delay := config.AppConfig.FileObjectDeleteDelay
	returned := false
	err := repo.Db.Transaction(func(tx *gorm.DB) error {
		var obj model.FileObject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", objectID).
			First(&obj).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		var actual int64
		if err := tx.Model(&model.UserFile{}).
			Where("object_id = ? AND is_deleted = 0 AND is_dir = 0", objectID).
			Count(&actual).Error; err != nil {
			return err
		}

		updates := map[string]interface{}{
			"ref_count": actual,
		}
		if actual > 0 {
			updates["delete_status"] = model.FileObjectDeleteStatusActive
			updates["delete_after"] = nil
		} else {
			updates["delete_status"] = model.FileObjectDeleteStatusPending
			if delay <= 0 {
				updates["delete_after"] = now
			} else {
				updates["delete_after"] = now.Add(delay)
			}
		}
		if err := tx.Model(&model.FileObject{}).Where("id = ?", objectID).Updates(updates).Error; err != nil {
			return err
		}
		setRefCountCache(ctx, objectID, actual)
		returned = true
		return nil
	})
	return returned, err
}

// StartFileObjectRefCountSyncWatchdog periodically flushes dirty ref_count cache to MySQL.
func StartFileObjectRefCountSyncWatchdog(ctx context.Context, interval time.Duration, batch int) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				synced, err := FlushFileObjectRefCounts(ctx, batch)
				if err != nil {
					log.Printf("file object ref sync failed: %v", err)
					continue
				}
				if synced > 0 {
					log.Printf("file object ref sync flushed %d objects", synced)
				}
			}
		}
	}()
}

// CleanupPendingFileObjects removes due file objects that are already marked as pending delete.
func CleanupPendingFileObjects(ctx context.Context, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 100
	}

	now := time.Now()
	var ids []uint64
	if err := repo.Db.Model(&model.FileObject{}).
		Where(
			"delete_status = ? AND delete_after IS NOT NULL AND delete_after <= ?",
			model.FileObjectDeleteStatusPending,
			now,
		).
		Order("delete_after ASC").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	cleaned := 0
	var firstErr error
	for _, id := range ids {
		ok, err := cleanupPendingFileObject(ctx, id, now)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if ok {
			cleaned++
		}
	}
	return cleaned, firstErr
}

func cleanupPendingFileObject(ctx context.Context, objectID uint64, now time.Time) (bool, error) {
	var (
		fileObject model.FileObject
		deleted    bool
	)
	err := repo.Db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", objectID).
			First(&fileObject).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var actual int64
		if err := tx.Model(&model.UserFile{}).
			Where("object_id = ? AND is_deleted = 0 AND is_dir = 0", fileObject.ID).
			Count(&actual).Error; err != nil {
			return err
		}
		setRefCountCache(ctx, fileObject.ID, actual)
		if actual > 0 {
			return tx.Model(&model.FileObject{}).
				Where("id = ?", fileObject.ID).
				Updates(map[string]interface{}{
					"ref_count":     actual,
					"delete_status": model.FileObjectDeleteStatusActive,
					"delete_after":  nil,
				}).Error
		}
		if fileObject.RefCount != 0 {
			if err := tx.Model(&model.FileObject{}).
				Where("id = ?", fileObject.ID).
				Update("ref_count", 0).Error; err != nil {
				return err
			}
		}
		if fileObject.DeleteStatus != model.FileObjectDeleteStatusPending {
			return nil
		}
		if fileObject.DeleteAfter == nil || fileObject.DeleteAfter.After(now) {
			return nil
		}
		if err := DeleteMinioFile(&fileObject); err != nil {
			return err
		}
		if err := tx.Delete(&model.FileObject{}, fileObject.ID).Error; err != nil {
			return err
		}
		deleted = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if !deleted {
		return false, nil
	}
	_ = utils.InvalidateFileObjectCache(context.Background(), fileObject.ID)
	_ = utils.InvalidateFileObjectHashCache(context.Background(), fileObject.Hash)
	_ = utils.InvalidateFileObjectPathCache(context.Background(), fileObject.BucketName, fileObject.ObjectName)
	if repo.Redis != nil {
		_ = repo.Redis.Del(context.Background(), fileObjectRefCountKey(fileObject.ID)).Err()
	}
	return true, nil
}

// StartFileObjectCleanupWatchdog periodically cleans due pending-delete file objects.
func StartFileObjectCleanupWatchdog(ctx context.Context, interval time.Duration, limit int) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleaned, err := CleanupPendingFileObjects(ctx, limit)
				if err != nil {
					log.Printf("file object cleanup failed: %v", err)
					continue
				}
				if cleaned > 0 {
					log.Printf("file object cleanup removed %d objects", cleaned)
				}
			}
		}
	}()
}
