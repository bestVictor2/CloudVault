package service

import (
	"Go_Pan/internal/repo"
	"Go_Pan/model"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FavoriteItem is one favorite file/folder with lightweight metadata.
type FavoriteItem struct {
	FileID    uint64     `json:"file_id"`
	Name      string     `json:"name"`
	IsDir     bool       `json:"is_dir"`
	Size      int64      `json:"size"`
	ParentID  *uint64    `json:"parent_id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// RecentItem is one recent access record.
type RecentItem struct {
	FileID       uint64    `json:"file_id"`
	Name         string    `json:"name"`
	IsDir        bool      `json:"is_dir"`
	Size         int64     `json:"size"`
	ParentID     *uint64   `json:"parent_id"`
	Source       string    `json:"source"`
	AccessCount  int64     `json:"access_count"`
	LastAccessAt time.Time `json:"last_access_at"`
}

// CommonDirItem is one directory ranked by recent usage.
type CommonDirItem struct {
	FileID       uint64    `json:"file_id"`
	Name         string    `json:"name"`
	ParentID     *uint64   `json:"parent_id"`
	AccessCount  int64     `json:"access_count"`
	LastAccessAt time.Time `json:"last_access_at"`
}

// AddFavorite creates a favorite entry for one file/folder.
func AddFavorite(userID, fileID uint64) error {
	if !CheckFileOwner(userID, fileID) {
		return gorm.ErrRecordNotFound
	}

	entry := &model.UserFavorite{
		UserID: userID,
		FileID: fileID,
	}
	return repo.Db.Clauses(clause.OnConflict{DoNothing: true}).Create(entry).Error
}

// RemoveFavorite removes one favorite entry.
func RemoveFavorite(userID, fileID uint64) error {
	return repo.Db.Where("user_id = ? AND file_id = ?", userID, fileID).Delete(&model.UserFavorite{}).Error
}

// ListFavorites lists favorites for one user.
func ListFavorites(userID uint64, limit int) ([]FavoriteItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	items := make([]FavoriteItem, 0)
	err := repo.Db.Table("user_favorite uf").
		Select("uf.file_id, f.name, f.is_dir, f.size, f.parent_id, uf.created_at, f.updated_at").
		Joins("JOIN user_file f ON f.id = uf.file_id").
		Where("uf.user_id = ? AND f.is_deleted = 0", userID).
		Order("uf.created_at DESC").
		Limit(limit).
		Scan(&items).Error
	return items, err
}

// RecordRecentAccess upserts one recent access item.
func RecordRecentAccess(userID, fileID uint64, source string) error {
	if userID == 0 || fileID == 0 {
		return nil
	}
	if !CheckFileOwner(userID, fileID) {
		return nil
	}

	now := time.Now()
	src := normalizeRecentSource(source)

	entry := &model.UserRecent{
		UserID:       userID,
		FileID:       fileID,
		Source:       src,
		AccessCount:  1,
		LastAccessAt: now,
	}
	return repo.Db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "file_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"source":         src,
			"last_access_at": now,
			"access_count":   gorm.Expr("access_count + ?", 1),
		}),
	}).Create(entry).Error
}

// ListRecent returns recent access records for one user.
func ListRecent(userID uint64, limit int) ([]RecentItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	items := make([]RecentItem, 0)
	err := repo.Db.Table("user_recent ur").
		Select("ur.file_id, f.name, f.is_dir, f.size, f.parent_id, ur.source, ur.access_count, ur.last_access_at").
		Joins("JOIN user_file f ON f.id = ur.file_id").
		Where("ur.user_id = ? AND f.is_deleted = 0", userID).
		Order("ur.last_access_at DESC").
		Limit(limit).
		Scan(&items).Error
	return items, err
}

// ListCommonDirs lists frequently visited directories based on recent accesses.
func ListCommonDirs(userID uint64, limit int) ([]CommonDirItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	items := make([]CommonDirItem, 0)
	err := repo.Db.Table("user_recent ur").
		Select("ur.file_id, f.name, f.parent_id, ur.access_count, ur.last_access_at").
		Joins("JOIN user_file f ON f.id = ur.file_id").
		Where("ur.user_id = ? AND f.is_deleted = 0 AND f.is_dir = 1", userID).
		Order("ur.access_count DESC, ur.last_access_at DESC").
		Limit(limit).
		Scan(&items).Error
	return items, err
}

func normalizeRecentSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return "unknown"
	}
	if len(source) > 64 {
		return source[:64]
	}
	return source
}
