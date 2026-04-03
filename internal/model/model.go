// Package model - provides all data-structs for the main application
package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CustomTime used for converting time fields from Kafka-JSON into time.Time
// пока не использую, так как gorm передает CustomTime в базу в виде текста, а не времени - пока не нашел решение
type CustomTime struct {
	time.Time
}

// Order is a complete model with embedded structs for storing order information received from Kafka
type Order struct {
	OrderUID    string `gorm:"primaryKey" json:"order_uid" faker:"-" validate:"required"`
	TrackNumber string `gorm:"not null" json:"track_number" faker:"word" validate:"required"`
	Entry       string `gorm:"not null" json:"entry" faker:"word" validate:"required"`

	Delivery Delivery `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;foreignKey:OrderUID;references:OrderUID" json:"delivery" validate:"required,dive"`
	Payment  Payment  `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;foreignKey:OrderUID;references:OrderUID" json:"payment" validate:"required,dive"`
	Items    []Item   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;foreignKey:OrderUID;references:OrderUID" json:"items" faker:"slice_len=2" validate:"required,min=1,dive"`

	Locale            string `gorm:"not null" json:"locale" faker:"word" validate:"required"`
	InternalSignature string `gorm:"not null" json:"internal_signature" faker:"word"`
	CustomerID        string `gorm:"not null" json:"customer_id" faker:"word" validate:"required"`
	DeliveryService   string `gorm:"not null" json:"delivery_service" faker:"word" validate:"required"`
	ShardKey          string `gorm:"not null" json:"shardkey" faker:"word" validate:"required"`
	SMID              int    `gorm:"not null" json:"sm_id" faker:"number" validate:"gte=1"`
	DateCreated       string `gorm:"not null" json:"date_created" faker:"date" validate:"required"` // ожидается формат "2021-11-26T06:22:19Z"
	OofShard          string `gorm:"not null" json:"oof_shard" faker:"word" validate:"required"`
}

// Delivery contains delivery information for a certain order
type Delivery struct {
	DID      *uint  `gorm:"primaryKey;autoIncrement;->" json:"-" faker:"-"`
	OrderUID string `gorm:"index;not null" faker:"-"` // FK на Order.OrderUID
	Name     string `gorm:"not null" json:"name" faker:"name" validate:"required"`
	Phone    string `gorm:"not null" json:"phone" faker:"-" validate:"required"`
	Zip      string `gorm:"not null" json:"zip" faker:"word" validate:"required"`
	City     string `gorm:"not null" json:"city" faker:"word" validate:"required"`
	Address  string `gorm:"not null" json:"address" faker:"word" validate:"required"`
	Region   string `gorm:"not null" json:"region"  faker:"word" validate:"required"`
	Email    string `gorm:"not null" json:"email" faker:"email" validate:"required,email"`
}

// Payment contains payment information for a certain order
type Payment struct {
	PID          *uint  `gorm:"primaryKey;autoIncrement;->" json:"-" faker:"-"`
	OrderUID     string `gorm:"index;not null;index" faker:"-"` // FK на Order.OrderUID
	Transaction  string `gorm:"not null" json:"transaction" faker:"word" validate:"required"`
	RequestID    string `gorm:"not null" json:"request_id" faker:"word" validate:"required"`
	Currency     string `gorm:"not null" json:"currency" faker:"word" validate:"required"`
	Provider     string `gorm:"not null" json:"provider" faker:"word" validate:"required"`
	Amount       uint   `gorm:"not null" json:"amount" faker:"number" validate:"gte=1"`     // должно быть суммой DeliveryCost+GoodsTotal+CustomFee
	PaymentDT    uint   `gorm:"not null" json:"payment_dt" faker:"number" validate:"gte=1"` // unix время в секундах
	Bank         string `gorm:"not null" json:"bank" faker:"word" validate:"required"`
	DeliveryCost uint   `gorm:"not null" json:"delivery_cost" faker:"number" validate:"gte=0"`
	GoodsTotal   uint   `gorm:"not null" json:"goods_total" validate:"gte=1"`
	CustomFee    uint   `gorm:"not null" json:"custom_fee" faker:"number" validate:"gte=0"`
}

// Item is a struct for items in an order, presented as an array in model.Order, cannot be empty(!)
type Item struct {
	IID         *uint  `gorm:"primaryKey;autoIncrement;->" json:"-" faker:"-"`
	OrderUID    string `gorm:"index;not null;index" faker:"-"` // FK на Order.OrderUID
	ChrtID      uint   `gorm:"not null" json:"chrt_id" faker:"number" validate:"gte=1"`
	TrackNumber string `gorm:"not null" json:"track_number" faker:"-" validate:"required"`
	Price       uint   `gorm:"not null" json:"price" faker:"number" validate:"gte=1"`
	RID         string `gorm:"not null" json:"rid" faker:"word" validate:"required"`
	Name        string `gorm:"not null" json:"name" faker:"word" validate:"required"`
	Sale        uint   `gorm:"not null" json:"sale" faker:"-" validate:"gte=0"` // ожидается процент скидки 0-100%
	Size        string `gorm:"not null" json:"size" faker:"word" validate:"required"`
	TotalPrice  uint   `gorm:"not null" json:"total_price" faker:"-" validate:"gte=1"` // будем считать, что итоговая цена товара округляется в меньшую сторону до рублей после применения скидки
	NMID        uint   `gorm:"not null" json:"nm_id" faker:"number" validate:"gte=1"`
	Brand       string `gorm:"not null" json:"brand" faker:"word" validate:"required"`
	Status      int    `gorm:"not null" json:"status" faker:"number" validate:"gte=0"`
}

// UnmarshalJSON - method for CustomTime used to process "RFC3339" and "Unix timestamp" input date types
func (ct *CustomTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		return nil
	}

	// пробуем как RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		ct.Time = t.UTC()
		return nil
	}

	// пробуем как Unix timestamp
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		ct.Time = time.Unix(ts, 0).UTC()
		return nil
	}

	return fmt.Errorf("неизвестный формат времени: %s", s)
}
