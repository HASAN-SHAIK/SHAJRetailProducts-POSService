package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/server"
)

func main() {
    cfg, err := config.Load()
    if err != nil {
        slog.Error("invalid configuration", "error", err)
        os.Exit(1)
    }

    startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancelStartup()

    db, err := database.Open(startupCtx, cfg.DatabasePath)
    if err != nil {
        slog.Error("open local database", "error", err)
        os.Exit(1)
    }
    defer db.Close()

    if err := db.Migrate(startupCtx); err != nil {
        slog.Error("apply local database migrations", "error", err)
        os.Exit(1)
    }
    if err := db.IntegrityCheck(startupCtx); err != nil {
        slog.Error("local database integrity check failed", "error", err)
        os.Exit(1)
    }

    deviceService := device.New(db)
    identity, err := deviceService.EnsureInstallation(startupCtx)
    if err != nil {
        slog.Error("initialize device identity", "error", err)
        os.Exit(1)
    }
    slog.Info("local device identity ready", "device_id", identity.DeviceID, "status", identity.Status)

    catalogRepository := catalog.NewRepository(db)
    customerRepository := customer.NewRepository(db)
    app := server.New(cfg, db, deviceService, catalogRepository, customerRepository)

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go func() {
        slog.Info("starting POS service", "address", cfg.ListenAddress, "database", cfg.DatabasePath)
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
