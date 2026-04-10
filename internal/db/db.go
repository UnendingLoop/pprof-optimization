// Package db - provides connection to database and AutoMigrate data model(if necessary) through gorm
package db

import (
	"log"

	"orderservice/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var models = []any{ // the order of tables is important
	&model.Order{},
	&model.Delivery{},
	&model.Payment{},
	&model.Item{},
}

// ConnectPostgres creates connection to Postres and runs automigration using structs from order.go
func ConnectPostgres(dsn string, mode string) *gorm.DB {
	var dbLog logger.Interface
	switch mode {
	case "production":
		dbLog = logger.Default.LogMode(logger.Silent) //отключаем логирование на уровне горма если режим выставлен как "продакшн"
	default:
	}

	orderDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: dbLog,
	})
	if err != nil {
		log.Fatalf("Cannot open db: %v", err)
	}
	if err := orderDB.AutoMigrate(models...); err != nil {
		log.Fatalf("Failed to migrate: %v", err)
	}
	log.Println("Connected to Postgres")
	return orderDB
}
