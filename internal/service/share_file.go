package service

import (
	"Go_Pan/internal/activity"
	"Go_Pan/internal/repo"
	"Go_Pan/model"
	"Go_Pan/utils"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

// CreateShare creates a share record and cache entry.
func CreateShare(userID, fileID uint64, expireDays int, needCode bool) (*model.FileShare, error) {
	if !CheckFileOwner(userID, fileID) {
		return nil, errors.New("permission denied")
	}

	var existingShare model.FileShare
	err := repo.Db.Where("file_id = ? AND user_id = ? AND status = 0", fileID, userID).
		Order("created_at DESC").
		First(&existingShare).Error
	if err == nil {
		if existingShare.ExpireAt == nil || time.Now().Before(*existingShare.ExpireAt) {
			return nil, errors.New("share already exists")
		}
		repo.Db.Model(&existingShare).Update("status", 1)
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	share := &model.FileShare{
		ShareID:  utils.GetToken(),
		FileID:   fileID,
		UserID:   userID,
		NeedCode: needCode,
		Status:   0,
	}
	if needCode {
		share.ExtractCode = utils.GenExtractCode()
	}
	if expireDays > 0 {
		expireAt := time.Now().Add(time.Duration(expireDays) * 24 * time.Hour)
		share.ExpireAt = &expireAt
	}

	if err := repo.Db.Create(share).Error; err != nil {
		return nil, err
	}

	if expireDays > 0 {
		key := "share:" + share.ShareID
		ttl := time.Until(*share.ExpireAt)
		value, _ := json.Marshal(share)
		log.Println("[CreateShare] redis db =", repo.Redis.Options().DB)
		log.Println("[CreateShare] set key =", key)
		repo.Redis.Set(context.Background(), key, value, ttl)
	}
	_ = activity.Emit(context.Background(), userID, activity.ActionShare, fileID, 0)

	return share, nil
}

// CheckShare validates a share and extract code.
func CheckShare(shareID, extractCode string) (*model.FileShare, error) {
	ctx := context.Background()
	key := "share:" + shareID

	val, err := repo.Redis.Get(ctx, key).Result()
	if err == redis.Nil {
		var share model.FileShare
		if err := repo.Db.Where("share_id = ? AND status = 0", shareID).First(&share).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, errors.New("share not found or expired")
			}
			return nil, err
		}

		if share.ExpireAt != nil && time.Now().After(*share.ExpireAt) {
			repo.Db.Model(&share).Update("status", 1)
			return nil, errors.New("share expired")
		}

		if share.NeedCode && share.ExtractCode != extractCode {
			return nil, errors.New("extract code mismatch")
		}

		return &share, nil
	}
	if err != nil {
		return nil, err
	}

	var share model.FileShare
	if err := json.Unmarshal([]byte(val), &share); err != nil {
		return nil, err
	}
	if share.NeedCode && share.ExtractCode != extractCode {
		return nil, errors.New("extract code mismatch")
	}

	return &share, nil
}
