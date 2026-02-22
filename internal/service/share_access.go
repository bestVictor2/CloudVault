package service

import (
	"CloudVault/internal/repo"
	"CloudVault/model"
	"net/url"
	"strings"
	"time"
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
