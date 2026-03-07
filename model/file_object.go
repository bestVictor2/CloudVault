package model

import "time"

type FileObject struct {
	ID uint64 `gorm:"primaryKey"`

	UserID uint64 `gorm:"column:user_id;not null;" json:"user_id,omitempty"`
	User   User   `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`

	Hash string `gorm:"column:hash;size:64;uniqueIndex;not null"`

	BucketName string `gorm:"column:bucket_name;size:64;not null"`
	ObjectName string `gorm:"column:object_name;size:512;not null"`

	Size int64 `gorm:"column:size;not null"`

	RefCount int `gorm:"column:ref_count;not null;default:1"`

	CreatedAt time.Time
}

// TableName returns the database table name.
func (FileObject) TableName() string {
	return "file_object"
}
