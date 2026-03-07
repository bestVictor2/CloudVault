package model

import "time"

type UploadSession struct {
	ID uint64 `gorm:"primaryKey"`

	UploadID string `gorm:"column:upload_id;size:36;uniqueIndex;not null"`

	UserID uint64 `gorm:"column:user_id;not null;index"`
	User   User   `gorm:"foreignKey:UserID;references:ID"`

	FileHash string `gorm:"column:file_hash;size:64;not null"`
	FileName string `gorm:"column:file_name;size:255;not null"`
	FileSize int64  `gorm:"column:file_size;not null"`

	ChunkSize   int64 `gorm:"column:chunk_size;not null"`
	TotalChunks int   `gorm:"column:total_chunks;not null"`

	Status int `gorm:"column:status;not null;default:0"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the database table name.
func (UploadSession) TableName() string {
	return "upload_session"
}
