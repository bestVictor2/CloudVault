package model

import (
	"gorm.io/gorm"
	"time"
)

type User struct {
	ID uint64 `gorm:"primaryKey"`

	UserName string `gorm:"column:user_name;type:varchar(50);not null;unique"`

	Password string `gorm:"column:pass_word;type:varchar(255);not null"`

	Email string `gorm:"column:email;type:varchar(255);not null;unique"`

	NickName  string `gorm:"column:nick_name;type:varchar(80);not null;default:''"`
	AvatarURL string `gorm:"column:avatar_url;type:varchar(512);not null;default:''"`
	Bio       string `gorm:"column:bio;type:varchar(500);not null;default:''"`

	IsActive bool `gorm:"column:is_active;not null;default:false"`

	TotalSpace uint64 `gorm:"column:total_space;not null;default:0"` // 容量管理
	UseSpace   uint64 `gorm:"column:use_space;not null;default:0"`
	CreatedAt  time.Time
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

// TableName returns the database table name.
func (User) TableName() string {
	return "user_db"
}
