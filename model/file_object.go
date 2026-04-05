package model

import "time"

type FileObject struct {
	ID uint64 `gorm:"primaryKey"`

	Hash string `gorm:"column:hash;size:64;uniqueIndex;not null"`

	BucketName string `gorm:"column:bucket_name;size:64;not null"`
	ObjectName string `gorm:"column:object_name;size:512;not null"`

	Size int64 `gorm:"column:size;not null"`

	RefCount int `gorm:"column:ref_count;not null;default:1"`

	DeleteStatus string     `gorm:"column:delete_status;size:32;not null;default:active;index:idx_file_object_delete"`
	DeleteAfter  *time.Time `gorm:"column:delete_after;index:idx_file_object_delete"`

	CreatedAt time.Time
}

const (
	FileObjectDeleteStatusActive  = "active"
	FileObjectDeleteStatusPending = "pending_delete"
)

// TableName returns the database table name.
func (FileObject) TableName() string {
	return "file_object"
}
