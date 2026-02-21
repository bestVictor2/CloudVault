package model

import "time"

// ShareAccessLog stores successful accesses to public share links.
type ShareAccessLog struct {
	ID uint64 `gorm:"primaryKey"`

	OwnerUserID uint64 `gorm:"column:owner_user_id;not null;index"`
	FileID      uint64 `gorm:"column:file_id;not null;index"`
	ShareID     string `gorm:"column:share_id;size:64;not null;index"`

	VisitorIP string `gorm:"column:visitor_ip;size:64;not null;default:'';index"`    // 访问者 ip
	Source    string `gorm:"column:source;size:128;not null;default:'direct';index"` // 访问来源
	Referer   string `gorm:"column:referer;type:text"`                               // 从哪个页面跳转
	UserAgent string `gorm:"column:user_agent;type:text"`                            // 浏览器 / 系统信息

	AccessedAt time.Time `gorm:"column:accessed_at;not null;index"`
	CreatedAt  time.Time
}

// TableName returns the database table name.
func (ShareAccessLog) TableName() string {
	return "share_access_log"
}
