package activity

import (
	"CloudVault/internal/repo"
	"CloudVault/model"
	"context"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	eventDedupPrefix = "activity:event:dedup"
	dailyRedisPrefix = "activity:daily"
	eventDedupTTL    = 24 * time.Hour
	dailyRedisTTL    = 14 * 24 * time.Hour
)

// DailySummary is the external view for user activity aggregates.
type DailySummary struct {
	Date          string `json:"date"`
	UploadCount   int64  `json:"upload_count"`
	UploadBytes   int64  `json:"upload_bytes"`
	DeleteCount   int64  `json:"delete_count"`
	DeleteBytes   int64  `json:"delete_bytes"`
	ShareCount    int64  `json:"share_count"`
	DownloadCount int64  `json:"download_count"`
	DownloadBytes int64  `json:"download_bytes"`
}

type increments struct {
	UploadCount   int64
	UploadBytes   int64
	DeleteCount   int64
	DeleteBytes   int64
	ShareCount    int64
	DownloadCount int64
	DownloadBytes int64
}

// ApplyEvent consumes one event and updates both MySQL and Redis.
func ApplyEvent(ctx context.Context, event *Event) error {
	if event == nil {
		return nil
	}
	if event.UserID == 0 {
		return nil
	}
	if _, ok := validActions[event.Action]; !ok {
		return nil
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	statDate := event.OccurredAt.Format("2006-01-02")
	inc := buildIncrements(event.Action, event.FileBytes)

	dedupKey := ""
	dedupSet := false
	if repo.Redis != nil && event.EventID != "" {
		dedupKey = eventDedupPrefix + ":" + event.EventID
		ok, err := repo.Redis.SetNX(ctx, dedupKey, "1", eventDedupTTL).Result()
		if err == nil {
			if !ok {
				return nil
			}
			dedupSet = true
		}
	}

	if err := upsertDailyRow(event.UserID, statDate, inc); err != nil {
		if dedupSet && repo.Redis != nil {
			_ = repo.Redis.Del(ctx, dedupKey).Err()
		}
		return err
	}

	if repo.Redis != nil {
		key := redisDailyKey(event.UserID, statDate)
		pipe := repo.Redis.TxPipeline()
		pipe.HIncrBy(ctx, key, "upload_count", inc.UploadCount)
		pipe.HIncrBy(ctx, key, "upload_bytes", inc.UploadBytes)
		pipe.HIncrBy(ctx, key, "delete_count", inc.DeleteCount)
		pipe.HIncrBy(ctx, key, "delete_bytes", inc.DeleteBytes)
		pipe.HIncrBy(ctx, key, "share_count", inc.ShareCount)
		pipe.HIncrBy(ctx, key, "download_count", inc.DownloadCount)
		pipe.HIncrBy(ctx, key, "download_bytes", inc.DownloadBytes)
		pipe.Expire(ctx, key, dailyRedisTTL)
		_, _ = pipe.Exec(ctx)
	}
	return nil
}

// GetSummary returns daily summaries for the latest N days.
// 采用 mysql 和 redis 两层数据设置 如果在 redis 中没有数据 那么则使用 mysql 的数据
func GetSummary(ctx context.Context, userID uint64, days int) ([]DailySummary, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	start := now.AddDate(0, 0, -(days - 1))

	var rows []model.UserActivityDaily
	if err := repo.Db.
		Where("user_id = ? AND stat_date >= ? AND stat_date <= ?", userID, start.Format("2006-01-02"), now.Format("2006-01-02")).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	byDate := make(map[string]model.UserActivityDaily, len(rows))
	for _, row := range rows {
		byDate[row.StatDate] = row
	}

	result := make([]DailySummary, 0, days)
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		row := byDate[date]
		item := DailySummary{
			Date:          date,
			UploadCount:   row.UploadCount,
			UploadBytes:   row.UploadBytes,
			DeleteCount:   row.DeleteCount,
			DeleteBytes:   row.DeleteBytes,
			ShareCount:    row.ShareCount,
			DownloadCount: row.DownloadCount,
			DownloadBytes: row.DownloadBytes,
		}
		if repo.Redis != nil {
			hash, err := repo.Redis.HGetAll(ctx, redisDailyKey(userID, date)).Result()
			if err == nil && len(hash) > 0 {
				item.UploadCount = parseInt64(hash["upload_count"], item.UploadCount)
				item.UploadBytes = parseInt64(hash["upload_bytes"], item.UploadBytes)
				item.DeleteCount = parseInt64(hash["delete_count"], item.DeleteCount)
				item.DeleteBytes = parseInt64(hash["delete_bytes"], item.DeleteBytes)
				item.ShareCount = parseInt64(hash["share_count"], item.ShareCount)
				item.DownloadCount = parseInt64(hash["download_count"], item.DownloadCount)
				item.DownloadBytes = parseInt64(hash["download_bytes"], item.DownloadBytes)
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func upsertDailyRow(userID uint64, statDate string, inc increments) error {
	row := &model.UserActivityDaily{
		UserID:        userID,
		StatDate:      statDate,
		UploadCount:   inc.UploadCount,
		UploadBytes:   inc.UploadBytes,
		DeleteCount:   inc.DeleteCount,
		DeleteBytes:   inc.DeleteBytes,
		ShareCount:    inc.ShareCount,
		DownloadCount: inc.DownloadCount,
		DownloadBytes: inc.DownloadBytes,
	}
	return repo.Db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "stat_date"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"upload_count":   gorm.Expr("upload_count + ?", inc.UploadCount),
			"upload_bytes":   gorm.Expr("upload_bytes + ?", inc.UploadBytes),
			"delete_count":   gorm.Expr("delete_count + ?", inc.DeleteCount),
			"delete_bytes":   gorm.Expr("delete_bytes + ?", inc.DeleteBytes),
			"share_count":    gorm.Expr("share_count + ?", inc.ShareCount),
			"download_count": gorm.Expr("download_count + ?", inc.DownloadCount),
			"download_bytes": gorm.Expr("download_bytes + ?", inc.DownloadBytes),
		}),
	}).Create(row).Error
}

func buildIncrements(action string, bytes int64) increments {
	if bytes < 0 {
		bytes = 0
	}
	switch action {
	case ActionUpload:
		return increments{
			UploadCount: 1,
			UploadBytes: bytes,
		}
	case ActionDelete:
		return increments{
			DeleteCount: 1,
			DeleteBytes: bytes,
		}
	case ActionShare:
		return increments{
			ShareCount: 1,
		}
	case ActionDownload:
		return increments{
			DownloadCount: 1,
			DownloadBytes: bytes,
		}
	default:
		return increments{}
	}
}

func redisDailyKey(userID uint64, statDate string) string {
	return dailyRedisPrefix + ":" + strconv.FormatUint(userID, 10) + ":" + statDate
}

func parseInt64(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}
