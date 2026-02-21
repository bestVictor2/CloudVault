package model

import (
	"time"

	"gorm.io/gorm"
)

type UserFile struct {
	ID uint64 `gorm:"primaryKey" json:"id,omitempty"`

	UserID uint64 `gorm:"column:user_id;not null;uniqueIndex:uk_user_parent_name,priority:1" json:"user_id,omitempty"`
	User   User   `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`

	ParentID *uint64   `gorm:"column:parent_id;index;uniqueIndex:uk_user_parent_name,priority:2" json:"parent_id,omitempty"`
	Parent   *UserFile `gorm:"foreignKey:ParentID;references:ID"`

	Name string `gorm:"column:name;size:255;not null;uniqueIndex:uk_user_parent_name,priority:3" json:"name,omitempty"`

	IsDir bool `gorm:"column:is_dir;not null;default:false" json:"is_dir,omitempty"`

	ObjectID *uint64     `gorm:"column:object_id;index" json:"object_id,omitempty"`
	Object   *FileObject `gorm:"foreignKey:ObjectID;references:ID" json:"object,omitempty"`

	Size int64 `gorm:"column:size;not null;default:0" json:"size,omitempty"`

	IsDeleted bool           `gorm:"column:is_deleted;default:false" json:"is_deleted,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
}

// TableName returns the database table name.
func (UserFile) TableName() string {
	return "user_file"
}

/*
关于数据库字段中指针与非指针的用法
在该文件中 NOT NULL 的字段可以设置为值类型 比如 UserID 而对于 ParentID 如果为根目录则不会对应有值 所以此处选择使用指针
Parent等 为关联对象 在此处也选择使用指针
在此处 User 为强关联字段 必须使用值类型 强关联类型也就是某个 UserFile 必定对应某个 User
*/
