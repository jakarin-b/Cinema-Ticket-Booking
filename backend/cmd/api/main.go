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

	appauth "github.com/cinema-ticket-booking/backend/internal/auth"
	"github.com/cinema-ticket-booking/backend/internal/bootstrap"
	"github.com/cinema-ticket-booking/backend/internal/httpapi"
	seatlock "github.com/cinema-ticket-booking/backend/internal/lock"
	"github.com/cinema-ticket-booking/backend/internal/observability"
	"github.com/cinema-ticket-booking/backend/internal/realtime"
	"github.com/cinema-ticket-booking/backend/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	deps, err := bootstrap.Load(ctx, true)
	if err != nil {
		slog.Error("API startup failed", "error", err)
		os.Exit(1)
	}
	defer deps.Close(context.Background())
	shutdownTracing, err := observability.InitTracing(ctx, deps.Config, "cinema-api")
	if err != nil {
		slog.Warn("tracing initialization failed", "error", err)
		shutdownTracing = func(context.Context) error { return nil }
	}
	defer shutdownTracing(context.Background())
	metrics := observability.NewMetrics()
	locks := seatlock.New(deps.Redis, deps.Config.SeatLockTTL)
	booking := service.NewBookingService(deps.Store, deps.Redis, locks, deps.Config, metrics)
	catalog := service.NewCatalogService(deps.Store)
	admin := service.NewAdminService(deps.Store)
	firebase, err := appauth.NewFirebase(ctx, deps.Config)
	if err != nil {
		slog.Error("Firebase initialization failed", "error", err)
		os.Exit(1)
	}
	googleOAuth := appauth.NewGoogleOAuth(deps.Redis, deps.Config)
	authService := appauth.NewService(deps.Store, deps.Redis, deps.Config, firebase)
	hub := realtime.NewHub(deps.Redis, deps.Config.AllowedOrigins, metrics)
	go hub.Run(ctx)
	handlers := httpapi.NewHandlers(deps.Config, deps.Store, deps.Redis, deps.Rabbit, authService, firebase, googleOAuth, catalog, booking, admin, hub)
	router := httpapi.Router(deps.Config, handlers, authService, deps.Redis, metrics)
	server := &http.Server{Addr: ":" + deps.Config.HTTPPort, Handler: router, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("API listening", "port", deps.Config.HTTPPort, "firebase_configured", firebase.Available(), "google_oauth_configured", googleOAuth.Available())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("API server stopped", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
