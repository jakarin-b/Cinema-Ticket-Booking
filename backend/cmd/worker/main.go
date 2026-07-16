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
	"github.com/cinema-ticket-booking/backend/internal/mailer"
	"github.com/cinema-ticket-booking/backend/internal/service"
	workerpkg "github.com/cinema-ticket-booking/backend/internal/worker"
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
	locks := seatlock.New(deps.Redis, deps.Config.SeatLockTTL)
	booking := service.NewBookingService(deps.Store, deps.Redis, locks, deps.Config)
	emailSender := mailer.NewSMTP(deps.Config)
	processor := workerpkg.NewProcessor(deps.Store, booking, deps.Rabbit, emailSender, deps.Config)
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"live"}`))
	})
	healthServer := &http.Server{
		Addr:              ":" + deps.Config.WorkerHealthPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && ctx.Err() == nil {
			slog.Error("worker health server stopped", "error", err)
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
	_ = healthServer.Shutdown(shutdownCtx)
}
