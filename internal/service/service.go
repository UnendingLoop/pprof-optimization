// Package service - layer with business-logics
package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"orderservice/internal/cache"
	"orderservice/internal/model"
	"orderservice/internal/repository"

	"github.com/go-playground/validator"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

type OrderService interface {
	AddNewOrder(msg *kafka.Message)
	AddNewOrdersBulk(msg []kafka.Message) error
	GetOrderInfo(ctx context.Context, uid string) (*model.Order, error)
}

// OrderService provides access to repo - DB operations, and contains a Map - cached orders
type orderService struct {
	Repo      repository.OrderRepository
	Map       *cache.OrderMap
	DLQwriter *kafka.Writer
}

var (
	ErrRecordNotFound = errors.New("запрошенный номер заказа не найден в базе")
	ErrJSONDecode     = errors.New("ошибка декодирования JSON-сообщения: ")
	ErrIncompleteJSON = errors.New("JSON содержит неполные данные")
	validateOrder     = validator.New() // Обработка ошибок валидации данных
)

// NewOrderService - returns *orderService
func NewOrderService(repo repository.OrderRepository, mapa *cache.OrderMap, broker, topic string) OrderService {
	dlqWriter := kafka.Writer{
		Addr:  kafka.TCP(broker),
		Topic: topic,
	}
	return &orderService{Repo: repo, Map: mapa, DLQwriter: &dlqWriter}
}

// AddNewOrder receives rawJson from Kafka consumer and creates new order in DB if rawJSON is valid, otherwise adds broken JSON into table InvalidRequests
func (OS *orderService) AddNewOrder(msg *kafka.Message) {
	var order model.Order
	// Обработка ошибки декодирования
	if err := json.Unmarshal(msg.Value, &order); err != nil {
		log.Printf(ErrJSONDecode.Error(), err)
		OS.pushToDLQ(msg.Value)
		return
	}

	err := validateOrder.Struct(order)
	if err != nil {
		for _, e := range err.(validator.ValidationErrors) {
			log.Printf("Order UID '%v': Поле '%s' не прошло проверку: %s\n", order.OrderUID, e.Field(), e.Tag())
		}
		OS.pushToDLQ(msg.Value)
		return
	}

	// Проверка на существование в кеше
	_, exists := OS.Map.CacheMap.Get(order.OrderUID)

	if exists {
		log.Printf("Заказ с номером '%s' уже существует!", order.OrderUID)
		return
	}

	// Записываем заказ в базу
	if err := OS.Repo.AddNewOrder(context.Background(), &order); err != nil {
		log.Printf("Failed to save order %s to DB: %v", order.OrderUID, err)
		return
	}
	// Обновление кеша
	OS.Map.CacheMap.Add(order.OrderUID, order)

	log.Printf("Order '%s' created and cached", order.OrderUID)
}

// AddNewOrdersBulk receives rawJson from Kafka consumer and creates new orders in DB if rawJSON is valid, otherwise adds broken JSON into table InvalidRequests
func (OS *orderService) AddNewOrdersBulk(msgBulk []kafka.Message) error {
	var ordersBulk []model.Order
	for _, raw := range msgBulk {
		// Обработка ошибки декодирования
		var order model.Order
		if err := json.Unmarshal(raw.Value, &order); err != nil {
			log.Printf(ErrJSONDecode.Error(), err)
			OS.pushToDLQ(raw.Value)
			continue
		}
		//валидация заказа
		if err := validateOrder.Struct(order); err != nil {
			for _, e := range err.(validator.ValidationErrors) {
				log.Printf("Order UID '%v': Поле '%s' не прошло проверку: %s\n", order.OrderUID, e.Field(), e.Tag())
			}
			OS.pushToDLQ(raw.Value)
			continue
		}
		// Проверка на существование в кеше
		if _, exists := OS.Map.CacheMap.Get(order.OrderUID); exists {
			log.Printf("Заказ с номером '%s' уже существует!", order.OrderUID)
			continue
		}

		ordersBulk = append(ordersBulk, order)
	}

	// Записываем заказы в БД
	if err := OS.Repo.AddNewOrdersBulk(context.Background(), ordersBulk); err != nil {
		log.Printf("Failed to save orders to DB: %v", err)
		return err
	}

	// Обновление кеша
	for _, o := range ordersBulk {
		OS.Map.CacheMap.Add(o.OrderUID, o)
		log.Printf("Order '%s' created and cached", o.OrderUID)
	}

	return nil
}

// GetOrderInfo used only for API-calls, returns model.Order by its uuid from DB if there is any, or nil and error
func (OS *orderService) GetOrderInfo(ctx context.Context, oid string) (*model.Order, error) {
	// Проверяем сначала кэш
	if cached, ok := OS.Map.CacheMap.Get(oid); ok {
		order := cached.(model.Order)
		return &order, nil
	}

	// В кеше нет, идем в бд:
	orderFromDB, err := OS.Repo.GetOrderByUID(ctx, oid)
	if err == nil {
		// Обновление кеша
		OS.Map.CacheMap.Add(oid, orderFromDB)
		return orderFromDB, nil
	}

	// Получили ошибку из бд
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	}
	return nil, err
}

func (OS *orderService) pushToDLQ(brokenJSON []byte) {
	err := OS.DLQwriter.WriteMessages(context.Background(), kafka.Message{
		Value: brokenJSON,
	})
	for err != nil {
		log.Printf("Failed to write to DLQtopic: %v\nRetrying...", err)
		time.Sleep(5 * time.Second)
		err = OS.DLQwriter.WriteMessages(context.Background(), kafka.Message{
			Value: brokenJSON,
		})
	}
	log.Printf("Invalid JSON successfully sent to DLQ.")
}

/*
func isValidOrderJSON(order *model.Order) bool {
	// Проверяем top-level поля Order
	if order.OrderUID == "" ||
		order.TrackNumber == "" ||
		order.Entry == "" ||
		order.Locale == "" ||
		order.CustomerID == "" ||
		order.DeliveryService == "" ||
		order.ShardKey == "" ||
		order.OofShard == "" ||
		order.DateCreated == "" {
		return false
	}

	// Проверяем Delivery
	d := order.Delivery
	if d.Name == "" ||
		d.Phone == "" ||
		d.Zip == "" ||
		d.City == "" ||
		d.Address == "" ||
		d.Region == "" ||
		d.Email == "" {
		return false
	}

	// Проверяем Payment
	p := order.Payment
	if p.Transaction == "" ||
		p.Currency == "" ||
		p.Provider == "" ||
		p.Amount == 0 ||
		p.PaymentDT == 0 ||
		p.Bank == "" ||
		p.GoodsTotal == 0 {
		return false
	}

	// Проверяем Items — массив не может быть пустым
	if len(order.Items) == 0 {
		return false
	}
	for _, item := range order.Items {
		if item.ChrtID == 0 ||
			item.TrackNumber == "" ||
			item.Price == 0 ||
			item.RID == "" ||
			item.Name == "" ||
			item.Size == "" ||
			item.TotalPrice == 0 ||
			item.NMID == 0 ||
			item.Brand == "" {
			return false
		}
	}
	return true
}
*/
