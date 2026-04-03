package cache

import (
	"context"
	"log"

	"orderservice/internal/repository"

	lru "github.com/hashicorp/golang-lru"
)

// OrderMap provides access to cache-map, contains embedded mutex features
type OrderMap struct {
	CacheMap *lru.Cache
	Repo     repository.OrderRepository
}

// CreateAndWarmUpOrderCache returns a new map with warmed up cache, access to DB and embedded mutex
func CreateAndWarmUpOrderCache(repo repository.OrderRepository, size int) (*OrderMap, error) {
	cache, err := lru.New(size)
	if err != nil {
		log.Printf("Failed to create lru-cache: %v", err)
		return nil, err
	}
	orderMap := OrderMap{Repo: repo}
	orders, err := orderMap.Repo.GetAllOrders(context.Background(), size)
	if err != nil {
		log.Printf("Failed to read orders from DB to warm up cahce: %v", err)
		return nil, err
	}

	for _, v := range orders {
		cache.Add(v.OrderUID, v)
	}

	orderMap.CacheMap = cache
	log.Println("Cache successfully loaded!")
	return &orderMap, nil
}
