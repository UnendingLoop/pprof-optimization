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

	batchSize := 100
	batch := []kafka.Message{}
	batchTimer := time.NewTicker(100 * time.Millisecond)
	ch := make(chan kafka.Message, batchSize*2)
	wgk := sync.WaitGroup{}
	wgk.Add(2)

	//запуск читателя из кафки
	go func() {
		defer wgk.Done()
		for {
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("Kafka read error: %v", err)
				continue
			}

			select {
			case <-ctx.Done():
				return
			case ch <- msg:
			}
		}
	}()

	//запуск батчера
	go func() {
		defer wgk.Done()
		for {
			select {
			case <-ctx.Done():
				if len(batch) != 0 { //обрабатываем последний накопленный батч перед выходом
					if err := srv.AddNewOrdersBulk(batch); err != nil {
						return
					}
					if err := reader.CommitMessages(ctx, batch...); err != nil {
						return
					}
				}
				return
			case <-batchTimer.C: //слив батча по таймауту
				if len(batch) != 0 {
					if err := srv.AddNewOrdersBulk(batch); err != nil {
						batch = batch[:0]
						continue
					}
					if err := reader.CommitMessages(ctx, batch...); err != nil {
						log.Println("Failed to commit kafka-message:", err)
					}
					batch = batch[:0]
				}
			case raw, ok := <-ch:
				if !ok {
					return
				}

				batch = append(batch, raw)

				if len(batch) >= batchSize {
					if err := srv.AddNewOrdersBulk(batch); err != nil {
						continue
					}
					if err := reader.CommitMessages(ctx, batch...); err != nil {
						log.Println("Failed to commit kafka-message:", err)
					}
					batch = batch[:0]
					batchTimer.Stop()
					batchTimer.Reset(100 * time.Millisecond)
				}
			}
		}
	}()

	wgk.Wait()
	//Освобождаем ресурсы
	batchTimer.Stop()
	close(ch)
}

// NewKafkaReader -
func NewKafkaReader(broker, topic string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{broker},
		Topic:       topic,
		GroupID:     "order-service",
		MinBytes:    1e3,
		MaxBytes:    1e7, // 10MB
		MaxWait:     500 * time.Millisecond,
		StartOffset: kafka.FirstOffset,
	})
}
