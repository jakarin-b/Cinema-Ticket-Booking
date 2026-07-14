package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/bootstrap"
	seedpkg "github.com/cinema-ticket-booking/backend/internal/seed"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	deps, err := bootstrap.Load(ctx, false)
	if err != nil {
		slog.Error("seed startup failed", "error", err)
		os.Exit(1)
	}
	defer deps.Close(context.Background())
	if err := seedpkg.Run(ctx, deps.Store); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
	slog.Info("seed completed")
}
