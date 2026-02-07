package model

import (
	"time"

	"gorm.io/gorm"
)

// Order 订单主表
type Order struct {
	ID          uint        `gorm:"primaryKey"`
	OrderNo     string      `gorm:"type:varchar(64);unique;not null"`
	UserID      int64       `gorm:"index;not null"`
	TotalAmount float64     `gorm:"type:decimal(10,2);not null"`
	Status      int32       `gorm:"default:0"` // 0:待支付 1:已支付
	Items       []OrderItem `gorm:"foreignKey:OrderID"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

// OrderItem 订单详情表
type OrderItem struct {
	ID          uint    `gorm:"primaryKey"`
	OrderID     uint    `gorm:"index;not null"`
	ProductID   int64   `gorm:"not null"`
	SkuID       int64   `gorm:"not null"`
	ProductName string  `gorm:"type:varchar(100);not null"`
	SkuName     string  `gorm:"type:varchar(100);not null"`
	Price       float64 `gorm:"type:decimal(10,2);not null"`
	Quantity    int32   `gorm:"not null"`
	Picture     string  `gorm:"type:varchar(255)"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (Order) TableName() string {
	return "orders"
}

func (OrderItem) TableName() string {
	return "order_items"
}
