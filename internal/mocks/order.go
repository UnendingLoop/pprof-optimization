// Package mocks provides function "GenerateMockOrder" for generating mock valid data to be marshalled and sent to Kafka
// No input data is needed: function uses order structure and its embedded subsctructures from kackage "model"
// Also provides consistency between structures like same OrderUID and corresponding total prices.
package mocks

import (
	"log"
	"reflect"

	"orderservice/internal/model"

	"github.com/go-faker/faker/v4"
)

// инициализируем кастомный тег "number"
func init() {
	err := faker.AddProvider("number", func(v reflect.Value) (interface{}, error) {
		number, _ := faker.RandomInt(300, 700)
		return uint(number[0]), nil
	})
	if err != nil {
		log.Fatal("Failed to initialize custom tag 'number' for faker:", err)
	}
}

// GenerateMockOrder - generates valid mock orders(one order per call) on the spot.
func GenerateMockOrder() *model.Order {
	var order model.Order
	if err := faker.FakeData(&order); err != nil {
		log.Printf("Failed faker order generation: %v", err)
	}

	order.OrderUID = "fake-" + faker.UUIDHyphenated()
	order.Delivery.Phone = faker.Phonenumber()

	conformOrderUID(&order)
	conformItems(order.Items)
	conformPayment(&order)

	return &order
}

func conformOrderUID(order *model.Order) {
	order.Delivery.OrderUID = order.OrderUID
	order.Payment.OrderUID = order.OrderUID

	for i := range order.Items {
		order.Items[i].OrderUID = order.OrderUID
		order.Items[i].TrackNumber = order.TrackNumber
	}
}

// просчитываем правильную итоговую сумму товаров в заказе с учетом скидки
func conformItems(items []model.Item) {
	for i := range items {
		items[i].Sale = 20
		items[i].TotalPrice = items[i].Price * (100 - items[i].Sale) / 100
	}
}

// просчитываем правильную итоговую сумму для model.Payment: товары, доставка и комиссия банка-эквайерера
func conformPayment(order *model.Order) {
	for i := range order.Items {
		order.Payment.GoodsTotal += order.Items[i].TotalPrice
	}
	order.Payment.Amount = order.Payment.GoodsTotal + order.Payment.DeliveryCost + order.Payment.CustomFee
}
