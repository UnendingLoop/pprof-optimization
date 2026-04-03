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
		Addr:  kafka.TCP(broker),
		Topic: topic,
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
		time.Sleep(1 * time.Second)
		counter++
		line := scanner.Bytes()
		err = mockWriter.WriteMessages(context.Background(), kafka.Message{
			Value: line,
		})
		for err != nil {
			log.Printf("Failed to publish test order #%d: %v", counter, err)
			log.Printf("Retrying to send order #%d...", counter)
			time.Sleep(5 * time.Second)
			err = mockWriter.WriteMessages(context.Background(), kafka.Message{
				Value: line,
			})
		}

		log.Printf("Order #%d published to Kafka", counter)
	}

	// начинаем генерацию 10 заказов через Faker
	fmt.Println("\nInitiating fake orders generation...")
	for i := range 10 {
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
			log.Printf("Retrying to send fake order #%d...", i)
			time.Sleep(5 * time.Second)
			err = mockWriter.WriteMessages(context.Background(), kafka.Message{
				Value: order,
			})
		}

		log.Printf("Fake order #%d published to Kafka\n", i)
		time.Sleep(2 * time.Second)
	}
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
