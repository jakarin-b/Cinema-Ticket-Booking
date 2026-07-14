package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/messaging"
	"github.com/redis/go-redis/v9"
)

type Dependencies struct {
	Config config.Config
	Store  *database.Store
	Redis  *redis.Client
	Rabbit *messaging.Rabbit
}

func Load(ctx context.Context, withRabbit bool) (*Dependencies, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	configureLogger(cfg)
	store, err := database.Connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}
	if err := store.EnsureIndexes(ctx); err != nil {
		_ = store.Close(ctx)
		return nil, fmt.Errorf("ensure indexes: %w", err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		_ = store.Close(ctx)
		return nil, fmt.Errorf("connect Redis: %w", err)
	}
	deps := &Dependencies{Config: cfg, Store: store, Redis: redisClient}
	if withRabbit {
		rabbit, err := messaging.Connect(cfg)
		if err != nil {
			_ = redisClient.Close()
			_ = store.Close(ctx)
			return nil, fmt.Errorf("connect RabbitMQ: %w", err)
		}
		deps.Rabbit = rabbit
	}
	return deps, nil
}

func (d *Dependencies) Close(ctx context.Context) {
	if d.Rabbit != nil {
		_ = d.Rabbit.Close()
	}
	if d.Redis != nil {
		_ = d.Redis.Close()
	}
	if d.Store != nil {
		_ = d.Store.Close(ctx)
	}
}
func configureLogger(cfg config.Config) {
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	if cfg.LogLevel == "warn" {
		level = slog.LevelWarn
	}
	if cfg.LogLevel == "error" {
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
