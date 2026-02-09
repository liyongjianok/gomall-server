package model

import "time"

// Order 订单主表
type Order struct {
	ID          uint    `gorm:"primaryKey"`
	OrderNo     string  `gorm:"type:varchar(64);uniqueIndex"`
	UserID      int64   `gorm:"index"`
	TotalAmount float64 `gorm:"type:decimal(10,2)"`
	Status      int     `gorm:"default:0"` // 0:待支付 1:已支付 2:已取消

	// [新增] 地址快照字段
	ReceiverName    string `gorm:"type:varchar(50)"`
	ReceiverMobile  string `gorm:"type:varchar(20)"`
	ReceiverAddress string `gorm:"type:varchar(255)"` // 省市区+详细地址的拼接

	Items     []OrderItem `gorm:"foreignKey:OrderID"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// OrderItem 订单明细表 (保持不变)
type OrderItem struct {
	ID          uint    `gorm:"primaryKey"`
	OrderID     uint    `gorm:"index"`
	ProductID   int64   `gorm:"index"`
	SkuID       int64   `gorm:"index"`
	ProductName string  `gorm:"type:varchar(100)"`
	SkuName     string  `gorm:"type:varchar(100)"`
	Price       float64 `gorm:"type:decimal(10,2)"`
	Quantity    int     `gorm:"type:int"`
	Picture     string  `gorm:"type:varchar(255)"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
