package model

import "gorm.io/gorm"

type User struct {
	gorm.Model        // 包含了 ID, CreatedAt, UpdatedAt, DeletedAt
	Username   string `gorm:"type:varchar(100);unique;not null"`
	Password   string `gorm:"type:varchar(255);not null"`
	Mobile     string `gorm:"type:varchar(20)"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}
