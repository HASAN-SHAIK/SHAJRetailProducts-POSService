package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/changefeed"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/security"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/server"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncengine"
)

func main() {
    cfg, err := config.Load()
    if err != nil { slog.Error("invalid configuration", "error", err); os.Exit(1) }

    startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancelStartup()

    db, err := database.Open(startupCtx, cfg.DatabasePath)
    if err != nil { slog.Error("open local database", "error", err); os.Exit(1) }
    defer db.Close()
    if err := db.Migrate(startupCtx); err != nil { slog.Error("apply local database migrations", "error", err); os.Exit(1) }
    if err := db.IntegrityCheck(startupCtx); err != nil { slog.Error("local database integrity check failed", "error", err); os.Exit(1) }

    deviceService := device.New(db)
    identity, err := deviceService.EnsureInstallation(startupCtx)
    if err != nil { slog.Error("initialize device identity", "error", err); os.Exit(1) }
    slog.Info("local device identity ready", "device_id", identity.DeviceID, "status", identity.Status)

    localAuth, err := security.LoadOrCreate(identity.DeviceID, cfg.LocalAPIToken, cfg.LocalTokenFile, cfg.AllowedOrigins)
    if err != nil { slog.Error("initialize local API security", "error", err); os.Exit(1) }

    catalogRepository := catalog.NewRepository(db)
    customerRepository := customer.NewRepository(db)
    orderService := orders.New(db, catalogRepository)
    paymentService := payments.New(db)
    inventoryService := inventory.New(db)
    receiptService := receipts.New(db)
    app := server.NewSecure(cfg, db, deviceService, catalogRepository, customerRepository, orderService, paymentService, inventoryService, receiptService, localAuth)

    eventOutbox := outbox.New(db)
    syncEngine, err := syncengine.New(eventOutbox, cfg.CentralAPIURL, identity.DeviceID, cfg.SyncRequestTimeout, cfg.SyncPollInterval)
    if err != nil { slog.Error("configure sync engine", "error", err); os.Exit(1) }

    inboxService := inbox.New(db)
    var changePuller *changefeed.Puller
    if cfg.CentralAPIURL != "" {
        changePuller = changefeed.New(db, inboxService, cfg.CentralAPIURL, identity.DeviceID, cfg.SyncRequestTimeout, cfg.SyncPollInterval)
    } else {
        slog.Info("central sync disabled", "reason", "POS_CENTRAL_API_URL is not configured")
    }

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    if syncEngine != nil { go syncEngine.Run(ctx) }
    if changePuller != nil { go changePuller.Run(ctx) }
    go func() {
        slog.Info("starting POS service", "address", cfg.ListenAddress, "database", cfg.DatabasePath)
        if err := app.Start(); err != nil { slog.Error("POS service stopped unexpectedly", "error", err); stop() }
    }()

    <-ctx.Done()
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := app.Shutdown(shutdownCtx); err != nil { slog.Error("graceful shutdown failed", "error", err); os.Exit(1) }
}
