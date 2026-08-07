package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/server"
)

func main() {
    cfg, err := config.Load()
    if err != nil {
        slog.Error("invalid configuration", "error", err)
        os.Exit(1)
    }

    app := server.New(cfg)

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go func() {
        slog.Info("starting POS service", "address", cfg.ListenAddress)
        if err := app.Start(); err != nil {
            slog.Error("POS service stopped unexpectedly", "error", err)
            stop()
        }
    }()

    <-ctx.Done()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := app.Shutdown(shutdownCtx); err != nil {
        slog.Error("graceful shutdown failed", "error", err)
        os.Exit(1)
    }
}
