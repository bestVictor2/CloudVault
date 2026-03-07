package model

import "time"

type DownloadTask struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	UserID uint64 `gorm:"column:user_id;index;not null" json:"user_id"`

	Type   string `gorm:"column:type;type:varchar(32);not null" json:"type"` // http / magnet / torrent / share
	Source string `gorm:"column:source;type:text;not null" json:"source"`

	Bucket     string `gorm:"column:bucket;type:varchar(64);not null" json:"bucket"`
	ObjectName string `gorm:"column:object_name;type:varchar(255);not null" json:"object_name"`
	FileName   string `gorm:"column:file_name;type:varchar(255);not null" json:"file_name"`

	Status      string     `gorm:"column:status;type:varchar(32);index;not null" json:"status"`
	Progress    int        `gorm:"column:progress;default:0" json:"progress"`
	ErrorMsg    string     `gorm:"column:error_msg;type:text" json:"error_msg"`
	RetryCount  int        `gorm:"column:retry_count;default:0" json:"retry_count"`
	NextRetryAt *time.Time `gorm:"column:next_retry_at" json:"next_retry_at"`
	StartedAt   *time.Time `gorm:"column:started_at" json:"started_at"`
	FinishedAt  *time.Time `gorm:"column:finished_at" json:"finished_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the database table name.
func (DownloadTask) TableName() string {
	return "download_task"
}
