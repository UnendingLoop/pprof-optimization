// Package repository - a layer of main application which interacts with DB using gorm-requests
package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"orderservice/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrderRepository -
type OrderRepository interface {
	AddNewOrder(ctx context.Context, neworder *model.Order) error
	AddNewOrdersBulk(ctx context.Context, newOrders []model.Order) error
	GetOrderByUID(ctx context.Context, uid string) (*model.Order, error)
	GetAllOrders(ctx context.Context, count int) ([]model.Order, error)
}

type orderRepository struct {
	DB           *gorm.DB
	dsn          string      // для переподключения если отвалилась база
	reconnecting atomic.Bool // флаг запущенного переподключения к БД
	sync.Mutex               // для предотвращения множественного вызова connectWithRetry из других экземпляров хендлеров при отвале БД
}

// NewOrderRepository -
func NewOrderRepository(db *gorm.DB, dsnDB string) OrderRepository {
	return &orderRepository{DB: db, dsn: dsnDB}
}

// GetOrderByUID finds order by its UUID and provides it with error message(if any)
func (OR *orderRepository) GetOrderByUID(ctx context.Context, uid string) (*model.Order, error) {
	var order model.Order
	for range 3 { // ограничимся тройным циклом вместо рекурсивного вызова всей AddNewOrder
		err := OR.DB.WithContext(ctx).Preload("Delivery").Preload("Payment").Preload("Items").Where("order_uid = ?", uid).First(&order).Error
		if err == nil { // если успешно - сразу выходим из цикла и функции
			return &order, nil
		}

		if isConnectionError(err) {
			switch OR.reconnecting.Load() {
			case true: // если ошибка соединения и уже запущено переподключение - ждем и пробуем снова
				time.Sleep(15 * time.Second)
				continue
			case false:
				if conErr := OR.connectWithRetry(); conErr != nil { // если не получилось восстановить соединение с одной попытки - выход из функции
					return nil, conErr
				}
				continue
			}
		}
		return nil, err
	}
	return &order, nil
}

// AddNewOrder creates a new record in DB using ctx and transaction
func (OR *orderRepository) AddNewOrder(ctx context.Context, newOrder *model.Order) error {
	var tx *gorm.DB
	newOrder.Delivery.DID = nil
	newOrder.Payment.PID = nil
	for i := range newOrder.Items {
		newOrder.Items[i].IID = nil
	}

	auxFunc := func() error {
		tx = OR.DB.WithContext(ctx).Begin()

		// Создаём заказ, если его ещё нет
		if err := tx.FirstOrCreate(&newOrder, model.Order{OrderUID: newOrder.OrderUID}).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Delivery
		newOrder.Delivery.OrderUID = newOrder.OrderUID
		if err := tx.FirstOrCreate(&newOrder.Delivery, model.Delivery{OrderUID: newOrder.OrderUID}).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Payment
		newOrder.Payment.OrderUID = newOrder.OrderUID
		if err := tx.FirstOrCreate(&newOrder.Payment, model.Payment{OrderUID: newOrder.OrderUID}).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Items — вставляем все новые элементы
		for i := range newOrder.Items {
			newOrder.Items[i].OrderUID = newOrder.OrderUID
		}
		if err := tx.Create(&newOrder.Items).Error; err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			return err
		}
		return nil
	}

	for range 3 {
		err := auxFunc()
		if err == nil {
			return nil
		}

		if isConnectionError(err) {
			switch OR.reconnecting.Load() {
			case true:
				time.Sleep(15 * time.Second)
				continue
			case false:
				if conErr := OR.connectWithRetry(); conErr != nil {
					return conErr
				}
				continue
			}
		}
		return err
	}
	return nil
}

// GetAllOrders retreives existing orders from DB with limit=count, used for warming up cache at app launch
func (OR *orderRepository) GetAllOrders(ctx context.Context, count int) ([]model.Order, error) {
	var orders []model.Order

	for range 3 { // ограничимся тройным циклом вместо рекурсивного вызова всей GetAllOrders
		err := OR.DB.WithContext(ctx).Preload("Delivery").Preload("Payment").Preload("Items").Order("date_created DESC").Limit(count).Find(&orders).Error
		if err == nil { // если успешно - сразу выходим из цикла и функции
			return orders, nil
		}

		if isConnectionError(err) {
			switch OR.reconnecting.Load() {
			case true: // если ошибка соединения и уже запущено переподключение - ждем и пробуем снова
				time.Sleep(15 * time.Second)
				continue
			case false:
				if conErr := OR.connectWithRetry(); conErr != nil { // если не получилось восстановить соединение с одной попытки - выход из функции
					return nil, conErr
				}
				continue
			}
		}
		return nil, err
	}
	return orders, nil
}

func (OR *orderRepository) connectWithRetry() error {
	OR.reconnecting.Store(true)
	defer OR.reconnecting.Store(false)

	OR.Lock()
	defer OR.Unlock()
	var db *gorm.DB
	var err error
	maxRetries := 3
	delay := 3 * time.Second

	if sqlDB, err := OR.DB.DB(); err == nil {
		if errPing := sqlDB.Ping(); errPing == nil {
			return nil // соединение уже живое
		}
	}

	for i := 0; i < maxRetries; i++ {
		log.Printf("#%d attempt reconnecting to DB...", i+1)
		db, err = gorm.Open(postgres.Open(OR.dsn), &gorm.Config{})
		if err == nil {
			sqlDB, _ := db.DB()
			pingErr := sqlDB.Ping()
			if pingErr == nil {
				OR.DB = db
				log.Println("Successfully reconnected!")
				return nil
			}
			err = pingErr
		}
		time.Sleep(delay)
	}

	return fmt.Errorf("could not reconnect after %d retries: %w", maxRetries, err)
}

func isConnectionError(err error) bool {
	return strings.Contains(err.Error(), "bad connection") ||
		strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connection reset")
}

func (OR *orderRepository) AddNewOrdersBulk(ctx context.Context, newOrders []model.Order) error {
	if len(newOrders) == 0 {
		return errors.New("empty collection of orders to create")
	}

	//cначала разделить входящий слайс на подслайсы по таблицам
	var tx *gorm.DB
	dels, pays, items := convertOrdersToSubEntities(newOrders)

	auxFunc := func() error {
		tx = OR.DB.WithContext(ctx).Begin()

		// Создаём отсутствующие заказы
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&newOrders).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Delivery
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&dels).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Payment
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&pays).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Items
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&items).Error; err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			return err
		}
		return nil
	}

	for range 3 {
		err := auxFunc()
		if err == nil {
			return nil
		}

		if isConnectionError(err) {
			switch OR.reconnecting.Load() {
			case true:
				time.Sleep(15 * time.Second)
				continue
			case false:
				if conErr := OR.connectWithRetry(); conErr != nil {
					return conErr
				}
				continue
			}
		}
		return err
	}
	return nil
}

func convertOrdersToSubEntities(input []model.Order) ([]model.Delivery, []model.Payment, []model.Item) {
	dels := make([]model.Delivery, 0, len(input))
	pays := make([]model.Payment, 0, len(input))
	items := make([]model.Item, 0, len(input))

	for _, o := range input {
		o.Delivery.DID = nil
		o.Delivery.OrderUID = o.OrderUID

		o.Payment.PID = nil
		o.Payment.OrderUID = o.OrderUID

		for i := range o.Items {
			o.Items[i].IID = nil
			o.Items[i].OrderUID = o.OrderUID
		}

		dels = append(dels, o.Delivery)
		pays = append(pays, o.Payment)
		items = append(items, o.Items...)
	}

	return dels, pays, items
}
