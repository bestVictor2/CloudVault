package service

import (
	"CloudVault/internal/activity"
	"CloudVault/internal/repo"
	"CloudVault/model"
	"CloudVault/utils"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ShareAccessMeta carries request-side metadata for share access logs.
type ShareAccessMeta struct {
	VisitorIP  string
	UserAgent  string
	Referer    string
	Source     string
	AccessedAt time.Time
}

// ShareAccessLogItem is one row in access log query results.
type ShareAccessLogItem struct {
	ID         uint64    `json:"id"`
	ShareID    string    `json:"share_id"`
	FileID     uint64    `json:"file_id"`
	FileName   string    `json:"file_name"`
	VisitorIP  string    `json:"visitor_ip"`
	Source     string    `json:"source"`
	Referer    string    `json:"referer"`
	AccessedAt time.Time `json:"accessed_at"`
}

// ShareSourceStat is grouped by source domain/channel.
type ShareSourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// ShareDailyStat is grouped by day.
type ShareDailyStat struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// ShareTopShareStat is grouped by share link.
type ShareTopShareStat struct {
	ShareID  string `json:"share_id"`
	FileID   uint64 `json:"file_id"`
	FileName string `json:"file_name"`
	Count    int64  `json:"count"`
}

// ShareAccessStats is the analytics payload for one owner.
type ShareAccessStats struct {
	Days        int                 `json:"days"`
	TotalVisits int64               `json:"total_visits"`
	UniqueIPs   int64               `json:"unique_ips"`
	BySource    []ShareSourceStat   `json:"by_source"`
	Daily       []ShareDailyStat    `json:"daily"`
	TopShares   []ShareTopShareStat `json:"top_shares"`
}

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

// LogShareAccess stores one successful access for a share link.
func LogShareAccess(share *model.FileShare, meta ShareAccessMeta) error {
	if share == nil {
		return nil
	}
	source := strings.TrimSpace(meta.Source)
	if source == "" {
		source = detectAccessSource(meta.Referer)
	}
	accessedAt := meta.AccessedAt
	if accessedAt.IsZero() {
		accessedAt = time.Now()
	}

	entry := &model.ShareAccessLog{
		OwnerUserID: share.UserID,
		FileID:      share.FileID,
		ShareID:     share.ShareID,
		VisitorIP:   strings.TrimSpace(meta.VisitorIP),
		Source:      source,
		Referer:     strings.TrimSpace(meta.Referer),
		UserAgent:   strings.TrimSpace(meta.UserAgent),
		AccessedAt:  accessedAt,
	}
	return repo.Db.Create(entry).Error
}

// ListShareAccessLogs returns recent access logs for the owner.
func ListShareAccessLogs(ownerUserID uint64, shareID string, limit int) ([]ShareAccessLogItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 300 {
		limit = 300
	}
	shareID = strings.TrimSpace(shareID)

	items := make([]ShareAccessLogItem, 0)
	query := repo.Db.Table("share_access_log l").
		Select("l.id, l.share_id, l.file_id, COALESCE(f.name, '[deleted]') AS file_name, l.visitor_ip, l.source, l.referer, l.accessed_at").
		Joins("LEFT JOIN user_file f ON f.id = l.file_id").
		Where("l.owner_user_id = ?", ownerUserID)

	if shareID != "" {
		query = query.Where("l.share_id = ?", shareID)
	}
	err := query.Order("l.accessed_at DESC").Limit(limit).Scan(&items).Error
	return items, err
}

// GetShareAccessStats returns grouped access stats for one owner.
func GetShareAccessStats(ownerUserID uint64, days int) (*ShareAccessStats, error) {
	if days <= 0 {
		days = 7
	}
	if days > 180 {
		days = 180
	}
	start := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	stats := &ShareAccessStats{
		Days:      days,
		BySource:  make([]ShareSourceStat, 0),
		Daily:     make([]ShareDailyStat, 0),
		TopShares: make([]ShareTopShareStat, 0),
	}

	if err := repo.Db.Table("share_access_log").
		Where("owner_user_id = ? AND accessed_at >= ?", ownerUserID, start).
		Count(&stats.TotalVisits).Error; err != nil {
		return nil, err
	}
	if err := repo.Db.Table("share_access_log").
		Where("owner_user_id = ? AND accessed_at >= ?", ownerUserID, start).
		Distinct("visitor_ip").
		Count(&stats.UniqueIPs).Error; err != nil {
		return nil, err
	}

	if err := repo.Db.Table("share_access_log").
		Where("owner_user_id = ? AND accessed_at >= ?", ownerUserID, start).
		Select("source, COUNT(1) AS count").
		Group("source").
		Order("count DESC").
		Scan(&stats.BySource).Error; err != nil {
		return nil, err
	}

	if err := repo.Db.Table("share_access_log").
		Where("owner_user_id = ? AND accessed_at >= ?", ownerUserID, start).
		Select("DATE(accessed_at) AS date, COUNT(1) AS count").
		Group("DATE(accessed_at)").
		Order("DATE(accessed_at) ASC").
		Scan(&stats.Daily).Error; err != nil {
		return nil, err
	}

	if err := repo.Db.Table("share_access_log l").
		Select("l.share_id, l.file_id, COALESCE(f.name, '[deleted]') AS file_name, COUNT(1) AS count").
		Joins("LEFT JOIN user_file f ON f.id = l.file_id").
		Where("l.owner_user_id = ? AND l.accessed_at >= ?", ownerUserID, start).
		Group("l.share_id, l.file_id, f.name").
		Order("count DESC").
		Limit(10).
		Scan(&stats.TopShares).Error; err != nil {
		return nil, err
	}

	return stats, nil
}

func detectAccessSource(referer string) string {
	ref := strings.TrimSpace(referer)
	if ref == "" {
		return "direct"
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return "unknown"
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "unknown"
	}
	host = strings.TrimPrefix(host, "www.")
	return host
}
