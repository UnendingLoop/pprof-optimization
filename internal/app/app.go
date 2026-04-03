// Package app conyáins the centralized launch of all necessary sublayers
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"orderservice/config"
	handler "orderservice/internal/api"
	"orderservice/internal/cache"
	"orderservice/internal/db"
	"orderservice/internal/kafka"
	"orderservice/internal/repository"
	"orderservice/internal/service"
	"orderservice/internal/web"

	"github.com/go-chi/chi/v5"
)

// App -
type App struct {
	cfg config.Config
	srv *http.Server
	sync.WaitGroup
}

// NewApplication -
func NewApplication(conf config.Config) *App {
	return &App{cfg: conf}
}

// Run -
func (a *App) Run() {
	// подключаемся к базе
	db := db.ConnectPostgres(a.cfg.DSN)
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to retrieve sql.DB: %v", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Println("Failed to close DB-connection:", err)
		}
	}()

	// создаем экземпляр repository и прогреваем кэш
	repo := repository.NewOrderRepository(db, a.cfg.DSN)
	orderMap, err := cache.CreateAndWarmUpOrderCache(repo, a.cfg.CacheSize)
	if err != nil {
		log.Fatalf("Failed to load cache: %v", err)
	}

	// создаем экземпляры слоя сервиса и хэндлера
	svc := service.NewOrderService(repo, orderMap, a.cfg.KafkaBroker, a.cfg.DLQTopic)
	hndlr := handler.OrderHandler{
		Service: svc,
	}

	// настраиваем роутер и грузим настройки сервера
	r := chi.NewRouter()
	r.Get("/order/{uid}", hndlr.GetOrderInfo)
	r.Get("/order/", hndlr.GetOrderInfo)
	a.srv = config.LoadSrvConfig(r, a.cfg.AppPort)

	// запускаем сервер в отдельной горутине, чтобы можно было:
	// - вызвать Shutdown сервера из слушателя прерываний
	// - выполнить последующий код
	a.Add(1)
	go launchServer(&a.WaitGroup, a.srv)

	// грузим шаблоны страниц для веба
	web.LoadTemplates()

	// ждем пока кафка запустится
	kafka.WaitKafkaReady(a.cfg.KafkaBroker)

	// Cоздаем топики
	kafka.InitKafkaTopics(a.cfg.KafkaBroker, a.cfg.Topic, a.cfg.DLQTopic)

	// запускаем консюмер для чтения из кафки
	ctx, kafkaCancel := context.WithCancel(context.Background())
	a.Add(1)
	go kafka.StartConsumer(ctx, hndlr.Service, a.cfg.KafkaBroker, a.cfg.Topic, &a.WaitGroup)
	time.Sleep(3 * time.Second)

	// запуск мокового писателя в кафку для теста
	if a.cfg.LaunchMockGenerator {
		go kafka.EmulateMsgSending(a.cfg.KafkaBroker, a.cfg.Topic)
	}

	// Starting shutdown signal listener
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM) // слушатель сигналов Ctrl+C и SIGTERM - когда убиваем контейнеры
	defer close(sig)

	a.Add(1)
	go launchInterruptListener(&a.WaitGroup, sig, kafkaCancel, a.srv)

	a.Wait()
	log.Println("Exiting application...")
}

func launchServer(wg *sync.WaitGroup, srv *http.Server) {
	defer wg.Done()
	fmt.Printf("Server running on http://localhost%s\n", srv.Addr)
	err := srv.ListenAndServe()
	if err != nil {
		switch {
		case errors.Is(err, http.ErrServerClosed):
			log.Println("Server gracefully stopping...")
		default:
			log.Fatalf("Server stopped: %v", err)
		}
	}
}

func launchInterruptListener(wg *sync.WaitGroup, sig chan os.Signal, kafkaCancel context.CancelFunc, srv *http.Server) {
	log.Print("Interruption listener is running...")
	defer wg.Done()
	<-sig
	log.Println("Interrupt received!!! Starting shutdown sequence...")
	// stop Kafka consumer:
	kafkaCancel()
	log.Println("Kafka consumer stopping...")
	// 5 seconds to stop HTTP-server:
	ctx, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer httpCancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
		return
	}
	log.Println("HTTP server stopped")
}
