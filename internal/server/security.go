package server

import (
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/security"
)

// NewSecure preserves the existing server construction while adding the local
// machine trust boundary around every route. Health/readiness exceptions and
// browser-origin validation are handled by LocalAuth.
func NewSecure(
    cfg config.Config,
    db *database.DB,
    deviceService *device.Service,
    catalogRepository *catalog.Repository,
    customerRepository *customer.Repository,
    orderService *orders.Service,
    paymentService *payments.Service,
    inventoryService *inventory.Service,
    receiptService *receipts.Service,
    localAuth *security.LocalAuth,
) *Server {
    s := New(cfg, db, deviceService, catalogRepository, customerRepository, orderService, paymentService, inventoryService, receiptService)
    handler := s.httpServer.Handler
    if localAuth != nil {
        handler = localAuth.Middleware(handler)
    }
    // Correlation is outermost so even machine-auth failures receive the same
    // bounded request ID in the response and structured completion log.
    s.httpServer.Handler = requestCorrelationMiddleware(handler)
    return s
}
