package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	handler "orderservice/internal/api"
	"orderservice/internal/model"
	"orderservice/internal/service"
	"orderservice/internal/web"
	"strings"
	"testing"

	"github.com/segmentio/kafka-go"
)

// MockOrderService реализует интерфейс service.OrderService
type MockOrderService struct {
	GetOrderInfoFn func(ctx context.Context, uid string) (*model.Order, error)
}

func (m *MockOrderService) GetOrderInfo(ctx context.Context, uid string) (*model.Order, error) {
	return m.GetOrderInfoFn(ctx, uid)
}

func (m *MockOrderService) AddNewOrder(msg *kafka.Message) {
	// просто пустышка
}

func TestGetOrderInfo(t *testing.T) {
	web.LoadTemplates()

	tests := []struct {
		name         string
		uid          string
		serviceFn    func(ctx context.Context, uid string) (*model.Order, error)
		wantBody     string
		wantHTTPCode int
	}{
		{
			name: "empty uid",
			uid:  "",
			serviceFn: func(ctx context.Context, uid string) (*model.Order, error) {
				return nil, nil
			},
			wantBody:     "<h2>Поиск заказа</h2>",
			wantHTTPCode: http.StatusOK,
		},
		{
			name: "order found",
			uid:  "123",
			serviceFn: func(ctx context.Context, uid string) (*model.Order, error) {
				return &model.Order{OrderUID: uid}, nil
			},
			wantBody:     "<h2>Информация по заказу</h2>",
			wantHTTPCode: http.StatusOK,
		},
		{
			name: "order not found",
			uid:  "404",
			serviceFn: func(ctx context.Context, uid string) (*model.Order, error) {
				return nil, service.ErrRecordNotFound
			},
			wantBody:     "Заказ с таким UID не найден",
			wantHTTPCode: http.StatusOK,
		},
		{
			name: "deadline exceeded",
			uid:  "timeout",
			serviceFn: func(ctx context.Context, uid string) (*model.Order, error) {
				return nil, context.DeadlineExceeded
			},
			wantBody:     "context deadline exceeded",
			wantHTTPCode: http.StatusRequestTimeout,
		},
		{
			name: "other error",
			uid:  "error",
			serviceFn: func(ctx context.Context, uid string) (*model.Order, error) {
				return nil, errors.New("oops")
			},
			wantBody:     "Ошибка при поиске заказа: oops",
			wantHTTPCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?uid="+tt.uid, nil)
			w := httptest.NewRecorder()

			h := &handler.OrderHandler{
				Service: &MockOrderService{GetOrderInfoFn: tt.serviceFn},
			}

			h.GetOrderInfo(w, req)
			resp := w.Result()

			if resp.StatusCode != tt.wantHTTPCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantHTTPCode)
			}

			body := w.Body.String()
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", body, tt.wantBody)
			}
		})
	}
}
