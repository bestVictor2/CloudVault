package model

import "time"

// UserActivityDaily stores per-user daily activity counters.
type UserActivityDaily struct {
	ID uint64 `gorm:"primaryKey"`

	UserID   uint64 `gorm:"not null;uniqueIndex:idx_user_activity_daily_user_date"`
	StatDate string `gorm:"type:date;not null;uniqueIndex:idx_user_activity_daily_user_date"`

	UploadCount   int64 `gorm:"not null;default:0"`
	UploadBytes   int64 `gorm:"not null;default:0"`
	DeleteCount   int64 `gorm:"not null;default:0"`
	DeleteBytes   int64 `gorm:"not null;default:0"`
	ShareCount    int64 `gorm:"not null;default:0"`
	DownloadCount int64 `gorm:"not null;default:0"`
	DownloadBytes int64 `gorm:"not null;default:0"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the database table name.
func (UserActivityDaily) TableName() string {
	return "user_activity_daily"
}
