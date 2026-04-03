// Package kafka - provides mock-producer and consumer, also creates topics
package kafka

import (
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

// InitKafkaTopics - cerates topics for orders: the main one and DLQ
func InitKafkaTopics(broker, topic, topicDLQ string) {
	topics := []kafka.TopicConfig{{
		Topic:             topic,
		NumPartitions:     3,
		ReplicationFactor: 1,
	}, {
		Topic:             topicDLQ,
		NumPartitions:     3,
		ReplicationFactor: 1,
	}}

	topicsCreated := false

	for !topicsCreated {
		conn, err := kafka.Dial("tcp", broker)
		if err != nil {
			log.Println("Failed to dial broker:", err)
			time.Sleep(5 * time.Second)
			continue
		}
		defer func() {
			if err := conn.Close(); err != nil {
				log.Println("Failed to close reader connection to Kafka:", err)
			}
		}()

		if err := conn.CreateTopics(topics...); err != nil {
			log.Println("Failed to create topics:", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("Topics successfully created!")
		topicsCreated = true
	}
}
