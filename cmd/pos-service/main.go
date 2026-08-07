package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/app"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    cfg, err := config.Load()
    if err != nil {
        logger.Error("invalid configuration", "error", err)
        os.Exit(1)
    }

    service := app.New(cfg, logger)

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go func() {
        if err := service.Start(); err != nil {
            logger.Error("service stopped", "error", err)
            stop()
        }
    }()

    <-ctx.Done()

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := service.Shutdown(shutdownCtx); err != nil {
        logger.Error("graceful shutdown failed", "error", err)
        os.Exit(1)
    }
}
