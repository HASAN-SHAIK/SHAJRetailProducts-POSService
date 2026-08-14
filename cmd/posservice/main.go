package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/backup"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/changefeed"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/effectiveconfig"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inbox"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/observability"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/security"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/server"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncengine"
)

func main() {
    cfg, err := config.Load(); if err != nil { slog.Error("invalid configuration", "error", err); os.Exit(1) }
    startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second); defer cancelStartup()
    db, err := database.Open(startupCtx, cfg.DatabasePath); if err != nil { slog.Error("open local database", "error", err); os.Exit(1) }; defer db.Close()
    if err := db.Migrate(startupCtx); err != nil { slog.Error("apply local database migrations", "error", err); os.Exit(1) }
    if err := db.IntegrityCheck(startupCtx); err != nil { slog.Error("local database integrity check failed", "error", err); os.Exit(1) }

    deviceService := device.New(db)
    identity, err := deviceService.EnsureInstallation(startupCtx); if err != nil { slog.Error("initialize device identity", "error", err); os.Exit(1) }
    slog.Info("local device identity ready", "device_id", identity.DeviceID, "status", identity.Status)
    localAuth, err := security.LoadOrCreate(identity.DeviceID, cfg.LocalAPIToken, cfg.LocalTokenFile, cfg.AllowedOrigins); if err != nil { slog.Error("initialize local API security", "error", err); os.Exit(1) }

    catalogRepository := catalog.NewRepository(db); customerRepository := customer.NewRepository(db); orderService := orders.New(db, catalogRepository)
    effectiveConfigStore := effectiveconfig.NewStore(db)
    orderService.SetPriceOverridePolicy(func(ctx context.Context) (bool, error) {
        return effectiveConfigStore.Bool(ctx, "billing.allow_price_override", false)
    })
    orderService.SetDiscountPolicy(func(ctx context.Context) (orders.DiscountPolicy, error) {
        allowed, err := effectiveConfigStore.Bool(ctx, "billing.allow_discount", false)
        if err != nil { return orders.DiscountPolicy{}, err }
        maxPercent, err := effectiveConfigStore.Float64(ctx, "billing.max_discount_percent", 20)
        if err != nil { return orders.DiscountPolicy{}, err }
        if maxPercent < 0 || maxPercent > 100 { return orders.DiscountPolicy{}, fmt.Errorf("billing.max_discount_percent must be between 0 and 100") }
        return orders.DiscountPolicy{Allowed: allowed, MaxPercent: maxPercent}, nil
    })
    paymentService := payments.New(db); inventoryService := inventory.New(db); receiptService := receipts.New(db)
    app := server.NewSecure(cfg, db, deviceService, catalogRepository, customerRepository, orderService, paymentService, inventoryService, receiptService, localAuth)

    eventOutbox := outbox.New(db)
    syncEngine, err := syncengine.New(eventOutbox, cfg.CentralAPIURL, cfg.CentralTenantID, cfg.CentralSyncToken, identity.DeviceID, cfg.SyncRequestTimeout, cfg.SyncPollInterval); if err != nil { slog.Error("configure sync engine", "error", err); os.Exit(1) }
    inboxService := inbox.New(db)
    var changePuller *changefeed.Puller
    var effectiveConfigService *effectiveconfig.Service
    if cfg.CentralAPIURL != "" {
        changePuller = changefeed.New(db, inboxService, cfg.CentralAPIURL, cfg.CentralTenantID, cfg.CentralSyncToken, identity.DeviceID, cfg.SyncRequestTimeout, cfg.SyncPollInterval)
        configClient, configErr := effectiveconfig.NewClient(cfg.CentralAPIURL, cfg.CentralTenantID, identity.DeviceID, cfg.CentralSyncToken, cfg.SyncRequestTimeout)
        if configErr != nil { slog.Error("configure effective configuration sync", "error", configErr); os.Exit(1) }
        effectiveConfigService = effectiveconfig.NewService(effectiveConfigStore, configClient, slog.Default(), time.Minute)
    } else { slog.Info("central sync disabled", "reason", "POS_CENTRAL_API_URL is not configured") }
    backupService := backup.New(db, cfg.BackupDirectory, cfg.BackupRetention)
    diagnostics := observability.New(db, eventOutbox, cfg.BackupDirectory)

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM); defer stop()
    if syncEngine != nil { go syncEngine.Run(ctx) }
    if changePuller != nil { go changePuller.Run(ctx) }
    if effectiveConfigService != nil && effectiveConfigService.Enabled() { go effectiveConfigService.Run(ctx) }
    go backupService.Run(ctx, cfg.BackupInterval)
    go diagnostics.Run(ctx, cfg.ObservabilityInterval)
    go func() { slog.Info("starting POS service", "address", cfg.ListenAddress, "database", cfg.DatabasePath); if err := app.Start(); err != nil { slog.Error("POS service stopped unexpectedly", "error", err); stop() } }()

    <-ctx.Done()
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel()
    if err := app.Shutdown(shutdownCtx); err != nil { slog.Error("graceful shutdown failed", "error", err); os.Exit(1) }
}
