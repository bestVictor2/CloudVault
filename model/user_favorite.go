package model

import "time"

// UserFavorite stores files a user pinned as favorites.
type UserFavorite struct {
	ID uint64 `gorm:"primaryKey"`

	UserID uint64 `gorm:"not null;uniqueIndex:uk_user_favorite,priority:1;index"`
	FileID uint64 `gorm:"not null;uniqueIndex:uk_user_favorite,priority:2;index"`

	CreatedAt time.Time
}

// TableName returns the database table name.
func (UserFavorite) TableName() string {
	return "user_favorite"
}
