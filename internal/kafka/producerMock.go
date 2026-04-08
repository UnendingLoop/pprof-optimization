package kafka

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"orderservice/internal/mocks"

	"github.com/segmentio/kafka-go"
)

// EmulateMsgSending used to emulate real messages flow to test the app in real-time with real DB;
// mock json-data is read from file and generated using faker-package
func EmulateMsgSending(broker, topic string) {
	mockWriter := kafka.Writer{
		Addr:         kafka.TCP(broker),
		Topic:        topic,
		BatchSize:    1,
		BatchTimeout: 5 * time.Millisecond,
		Async:        false,
	}

	// чтение заказов из json-файла - 5 валидных, 2 дубликата и 3 невалидных
	file, err := os.Open("./internal/kafka/mocks.jsonl")
	if err != nil {
		log.Fatalf("Failed to open json-mocks file: %v\nExiting application.", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Println("Failed to close mock-file:", err)
		}
	}()

	scanner := bufio.NewScanner(file)
	counter := 0
	for scanner.Scan() {
		counter++
		line := scanner.Bytes()
		err = mockWriter.WriteMessages(context.Background(), kafka.Message{
			Value: line,
		})
		for err != nil {
			log.Printf("Failed to publish test order #%d: %v", counter, err)
			time.Sleep(100 * time.Millisecond)
			log.Printf("Retrying to send order #%d...", counter)
			err = mockWriter.WriteMessages(context.Background(), kafka.Message{
				Value: line,
			})
		}

		log.Printf("Order #%d published to Kafka", counter)
	}

	// начинаем генерацию 15000 заказов через Faker
	fmt.Println("\nInitiating fake orders generation...")
	time.Sleep(1 * time.Second)

	for i := range 15000 {
		order, err := json.Marshal(mocks.GenerateMockOrder())
		if err != nil {
			log.Printf("Fake order marshalling #%d failed: %v", i, err)
			continue
		}

		err = mockWriter.WriteMessages(context.Background(), kafka.Message{
			Value: order,
		})
		for err != nil {
			log.Printf("Failed to publish fake order #%d: %v", i, err)
			time.Sleep(100 * time.Millisecond)
			log.Printf("Retrying to send fake order #%d...", i)
			err = mockWriter.WriteMessages(context.Background(), kafka.Message{
				Value: order,
			})
		}
		log.Printf("Fake order #%d published to Kafka\n", i)
	}

	fmt.Println("\nFinished fake orders generation!!!")
}

// WaitKafkaReady - timeout given to kafka-service for getting fully functional
func WaitKafkaReady(broker string) {
	for {
		conn, err := kafka.Dial("tcp", broker)
		if err == nil {
			if errConn := conn.Close(); errConn != nil {
				log.Println("Failed to close connection after testing Kafka readyness:", errConn)
			}
			break
		}
		log.Println("Kafka not ready, retrying in 5s...")
		time.Sleep(5 * time.Second)
	}
	time.Sleep(25 * time.Second)
}
