package kafka

import (
	"bufio"
	"context"
	"log"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

// EmulateMsgSending used to emulate real messages flow to test the app in real-time with real DB;
// mock json-data is read from file and generated using faker-package
func EmulateMsgSending(broker, topic string) {
	mockWriter := kafka.Writer{
		Addr:         kafka.TCP(broker),
		Topic:        topic,
		BatchSize:    500,
		BatchBytes:   1e6,
		BatchTimeout: 20 * time.Millisecond,
		Async:        false,
	}

	// чтение 15000 фейковых заказов из json-файла
	file, err := os.Open("./internal/kafka/newMockFinal.jsonl")
	if err != nil {
		log.Fatalf("Failed to open json-mocks file: %v\nExiting application.", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Println("Failed to close mock-file:", err)
		}
	}()

	sharedFakeOrders := make([][]byte, 0, 15001)

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if err := scanner.Err(); err != nil {
			log.Printf("Error reading line: %v", err)
			continue
		}
		sharedFakeOrders = append(sharedFakeOrders, line)
	}
	log.Printf("Успешно прочитано %d строк из файла!", len(sharedFakeOrders))

	log.Println("Нагрузка через")
	for i := 5; i > 0; i-- {
		time.Sleep(1 * time.Second)
		log.Println(i, "...")
	}

	//Нагрузочное тестирование - 15К фейковых заказов через 5 горутин
	workersN := 5
	max := len(sharedFakeOrders)
	discretion := max / workersN

	for i := range workersN {
		go func(n int) {
			start := discretion * n
			end := start + discretion
			log.Printf("Goroutine #%d initiates sending messages to Kafka on indices [%d:%d]...", n, start, end)
			counter := 0
			for l, line := range sharedFakeOrders[start:end] {
				err = mockWriter.WriteMessages(context.Background(), kafka.Message{
					Value: line,
				})
				for err != nil {
					log.Printf("Failed to publish fake order #%d: %v", l, err)
					time.Sleep(50 * time.Millisecond)
					log.Printf("Retrying to send order #%d...", l)
					err = mockWriter.WriteMessages(context.Background(), kafka.Message{
						Value: line,
					})
				}
				counter++
				log.Printf("Goroutine #%d sent order #%d - success!", n, l)
			}
			log.Printf("Goroutine #%d finished sending %d messages!", n, counter)
		}(i)
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
