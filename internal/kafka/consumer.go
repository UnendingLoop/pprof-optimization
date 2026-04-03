package kafka

import (
	"context"
	"log"
	"sync"
	"time"

	"orderservice/internal/service"

	"github.com/segmentio/kafka-go"
)

// StartConsumer initializes listening to Kafka messages, which will be forwarded to Service-layer
func StartConsumer(ctx context.Context, srv service.OrderService, broker, topic string, wg *sync.WaitGroup) {
	defer wg.Done()
	reader := NewKafkaReader(broker, topic)
	defer func() {
		if err := reader.Close(); err != nil {
			log.Println("Failed to close Kafa-reader:", err)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				log.Printf("Kafka read error: %v", err)
				continue
			}
			srv.AddNewOrder(&msg)
			if err := reader.CommitMessages(ctx, msg); err != nil {
				log.Println("Failed to commit kafka-message:", err)
			}
		}
	}
}

// NewKafkaReader -
func NewKafkaReader(broker, topic string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{broker},
		Topic:       topic,
		GroupID:     "order-service",
		MinBytes:    10e3,
		MaxBytes:    10e6,
		StartOffset: kafka.FirstOffset,
		MaxWait:     1 * time.Second,
	})
}
