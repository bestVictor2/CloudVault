package model

import (
	"time"

	"gorm.io/gorm"
)

type FileShare struct {
	ID uint64 `gorm:"primaryKey"`

	ShareID string `gorm:"column:share_id;size:64;uniqueIndex;not null"`

	FileID uint64   `gorm:"column:file_id;not null;index"`
	File   UserFile `gorm:"foreignKey:FileID;references:ID;constraint:OnDelete:CASCADE"`

	UserID uint64 `gorm:"column:user_id;not null;index"`
	User   User   `gorm:"foreignKey:UserID;references:ID"`

	NeedCode    bool       `gorm:"column:need_code"`
	ExtractCode string     `gorm:"column:extract_code;size:10"`
	ExpireAt    *time.Time `gorm:"column:expire_at"`
	Status      int        `gorm:"column:status;not null"` // 0 normal

	CreatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// TableName returns the database table name.
func (FileShare) TableName() string {
	return "file_share"
}
