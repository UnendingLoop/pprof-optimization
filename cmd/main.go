package main

import (
	"fmt"

	"orderservice/config"
	"orderservice/internal/app"
)

func main() {
	// читаем конфиг из env
	startConfig := config.GetConfig()
	fmt.Println("Start config values:", startConfig)

	// запускаем приложение
	application := app.NewApplication(startConfig)
	application.Run()
}
