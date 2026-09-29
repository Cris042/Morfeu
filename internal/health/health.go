package health

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Redis  string `json:"redis"`
	// RabbitMQ só aparece quando o binário roda com broker (-mode=worker|all, RF09 do
	// PRD 0005); em -mode=api o JSON segue idêntico ao contrato da E0a.
	RabbitMQ string `json:"rabbitmq,omitempty"`
}

// BrokerChecker é o que o health precisa do broker: saber se há conexão ativa.
type BrokerChecker interface {
	Conectado() bool
}

// HealthHandler handles health check requests
type HealthHandler struct {
	db     *pgxpool.Pool
	redis  redis.Cmdable
	broker BrokerChecker
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(db *pgxpool.Pool, redis redis.Cmdable) *HealthHandler {
	return &HealthHandler{
		db:    db,
		redis: redis,
	}
}

// WithBroker inclui o RabbitMQ no health (RF09 do PRD 0005).
func (h *HealthHandler) WithBroker(b BrokerChecker) *HealthHandler {
	h.broker = b
	return h
}

// Check handles GET /health request
func (h *HealthHandler) Check(c echo.Context) error {
	dbStatus := h.checkDB()
	redisStatus := h.checkRedis()

	response := HealthResponse{
		Status: "ok",
		DB:     dbStatus,
		Redis:  redisStatus,
	}
	if h.broker != nil {
		response.RabbitMQ = "error"
		if h.broker.Conectado() {
			response.RabbitMQ = "ok"
		}
	}

	// Return 200 even if services are degraded
	return c.JSON(http.StatusOK, response)
}

// checkDB checks database connectivity
func (h *HealthHandler) checkDB() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		return "error"
	}
	return "ok"
}

// checkRedis checks Redis connectivity
func (h *HealthHandler) checkRedis() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.redis.Ping(ctx).Err(); err != nil {
		return "error"
	}
	return "ok"
}
