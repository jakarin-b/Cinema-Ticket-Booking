package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/bootstrap"
	seatlock "github.com/cinema-ticket-booking/backend/internal/lock"
	"github.com/cinema-ticket-booking/backend/internal/observability"
	"github.com/cinema-ticket-booking/backend/internal/service"
	workerpkg "github.com/cinema-ticket-booking/backend/internal/worker"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	deps, err := bootstrap.Load(ctx, true)
	if err != nil {
		slog.Error("worker startup failed", "error", err)
		os.Exit(1)
	}
	defer deps.Close(context.Background())
	shutdownTracing, err := observability.InitTracing(ctx, deps.Config, "cinema-worker")
	if err != nil {
		slog.Warn("tracing initialization failed", "error", err)
		shutdownTracing = func(context.Context) error { return nil }
	}
	defer shutdownTracing(context.Background())
	metrics := observability.NewMetrics()
	locks := seatlock.New(deps.Redis, deps.Config.SeatLockTTL)
	booking := service.NewBookingService(deps.Store, deps.Redis, locks, deps.Config, metrics)
	processor := workerpkg.NewProcessor(deps.Store, booking, deps.Rabbit, deps.Config, metrics)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"live"}`))
	})
	metricsServer := &http.Server{
		Addr:              ":" + deps.Config.WorkerMetricsPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && ctx.Err() == nil {
			slog.Error("worker metrics server stopped", "error", err)
			stop()
		}
	}()
	go processor.RunExpiration(ctx)
	go processor.RunOutbox(ctx)
	go func() {
		if err := processor.RunNotifications(ctx); err != nil && ctx.Err() == nil {
			slog.Error("notification consumer stopped", "error", err)
			stop()
		}
	}()
	slog.Info("worker started")
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = metricsServer.Shutdown(shutdownCtx)
}
