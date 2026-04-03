// Package db - provides connection to database and AutoMigrate data model(if necessary) through gorm
package db

import (
	"log"

	"orderservice/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var models = []any{ // the order of tables is important
	&model.Order{},
	&model.Delivery{},
	&model.Payment{},
	&model.Item{},
}

// ConnectPostgres creates connection to Postres and runs automigration using structs from order.go
func ConnectPostgres(dsn string) *gorm.DB {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Cannot open db: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		log.Fatalf("Failed to migrate: %v", err)
	}
	log.Println("Connected to Postgres")
	return db
}
