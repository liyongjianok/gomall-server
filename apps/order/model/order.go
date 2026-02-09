package model

import "time"

// Order 订单主表
type Order struct {
	ID          uint        `gorm:"primaryKey"`
	OrderNo     string      `gorm:"type:varchar(64);uniqueIndex"`
	UserID      int64       `gorm:"index"` // 注意：这里是 UserID (大写ID)
	TotalAmount float64     `gorm:"type:decimal(10,2)"`
	Status      int         `gorm:"default:0"` // 0:待支付 1:已支付
	Items       []OrderItem `gorm:"foreignKey:OrderID"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// OrderItem 订单明细表
type OrderItem struct {
	ID          uint    `gorm:"primaryKey"`
	OrderID     uint    `gorm:"index"`
	ProductID   int64   `gorm:"index"` // 注意：ProductID
	SkuID       int64   `gorm:"index"` // 注意：SkuID
	ProductName string  `gorm:"type:varchar(100)"`
	SkuName     string  `gorm:"type:varchar(100)"`
	Price       float64 `gorm:"type:decimal(10,2)"`
	Quantity    int     `gorm:"type:int"` // 这里保持 int，但在 main.go 里需要强转
	Picture     string  `gorm:"type:varchar(255)"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
