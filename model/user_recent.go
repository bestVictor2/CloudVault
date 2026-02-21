package model

import "time"

// UserRecent stores recently accessed files/folders for one user.
type UserRecent struct {
	ID uint64 `gorm:"primaryKey"`

	UserID uint64 `gorm:"not null;uniqueIndex:uk_user_recent,priority:1;index"`
	FileID uint64 `gorm:"not null;uniqueIndex:uk_user_recent,priority:2;index"`

	Source       string    `gorm:"type:varchar(64);not null;default:'unknown'"` // 来源
	AccessCount  int64     `gorm:"not null;default:0"`                          // 访问次数
	LastAccessAt time.Time `gorm:"not null;index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the database table name.
func (UserRecent) TableName() string {
	return "user_recent"
}
