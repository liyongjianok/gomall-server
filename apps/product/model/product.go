package model

import "gorm.io/gorm"

// Product 商品 SPU
type Product struct {
	gorm.Model
	Name        string  `gorm:"type:varchar(100);not null"`
	Description string  `gorm:"type:text"`
	CategoryID  int64   `gorm:"not null"`
	Picture     string  `gorm:"type:varchar(255)"`
	Price       float32 `gorm:"type:decimal(10,2)"` // 这里的 Price 是展示底价
}

// Category 商品分类
type Category struct {
	gorm.Model
	Name     string `gorm:"type:varchar(50);not null"`
	ParentID int64  `gorm:"default:0"`
}

// Sku 商品规格
type Sku struct {
	gorm.Model
	ProductID int64   `gorm:"not null;index"`
	Name      string  `gorm:"type:varchar(100);not null"` // 例如：3斤尝鲜装
	Price     float32 `gorm:"type:decimal(10,2)"`
	Stock     int     `gorm:"not null;default:0"`
	Picture   string  `gorm:"type:varchar(255)"`
}

func (Product) TableName() string {
	return "products"
}

func (Category) TableName() string {
	return "categories"
}

func (Sku) TableName() string {
	return "skus"
}
