package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/config"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/customer"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/inventory"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/localauth"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/observability"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/orders"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/payments"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/receipts"
)

type Server struct {
	httpServer *http.Server
	startedAt  time.Time
	cfg        config.Config
	db         *database.DB
	device     *device.Service
	catalog    *catalog.Repository
	customers  *customer.Repository
	orders     *orders.Service
	payments   *payments.Service
	inventory  *inventory.Service
	receipts   *receipts.Service
	localAuth  *localauth.Service
}

func New(cfg config.Config, db *database.DB, deviceService *device.Service, catalogRepository *catalog.Repository, customerRepository *customer.Repository, orderService *orders.Service, paymentService *payments.Service, inventoryService *inventory.Service, receiptService *receipts.Service) *Server {
	s := &Server{cfg: cfg, db: db, device: deviceService, catalog: catalogRepository, customers: customerRepository, orders: orderService, payments: paymentService, inventory: inventoryService, receipts: receiptService, localAuth: localauth.New(db, cfg.OfflineGrantSecret), startedAt: time.Now().UTC()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/ready", s.handleReady)
	mux.HandleFunc("POST /api/v1/auth/enroll", s.handleLocalAuthEnroll)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLocalAuthLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLocalAuthLogout)
	mux.HandleFunc("POST /api/v1/auth/approvals", s.handleManagerApproval)
	mux.HandleFunc("GET /api/v1/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("GET /api/v1/diagnostics/sync-events", s.handleSyncEventDiagnostics)
	mux.HandleFunc("GET /api/v1/device", s.handleGetDevice)
	mux.HandleFunc("PUT /api/v1/device/registration", s.handleDeviceRegistration)
	mux.HandleFunc("POST /api/v1/device/heartbeat", s.handleDeviceHeartbeat)
	mux.HandleFunc("GET /api/v1/catalog/products", requirePermission("products:read", s.handleCatalogSearch))
	mux.HandleFunc("GET /api/v1/catalog/products/barcode/{barcode}", requirePermission("products:read", s.handleCatalogBarcode))
	mux.HandleFunc("GET /api/v1/catalog/products/{id}", requirePermission("products:read", s.handleCatalogProduct))
	mux.HandleFunc("GET /api/v1/catalog/categories", requirePermission("products:read", s.handleCatalogCategories))
	mux.HandleFunc("GET /api/v1/customers", requirePermission("customers:read", s.handleCustomerSearch))
	mux.HandleFunc("POST /api/v1/customers", requirePermission("customers:write", s.handleCustomerCreate))
	mux.HandleFunc("GET /api/v1/customers/{id}", requirePermission("customers:read", s.handleCustomerGet))
	mux.HandleFunc("PUT /api/v1/customers/{id}", requirePermission("customers:write", s.handleCustomerUpdate))
	mux.HandleFunc("GET /api/v1/orders", requirePermission("orders:read", s.handleOrderList))
	mux.HandleFunc("POST /api/v1/orders", s.requireOrderWrite(s.handleOrderCreate))
	mux.HandleFunc("GET /api/v1/orders/{id}", requirePermission("orders:read", s.handleOrderGet))
	mux.HandleFunc("POST /api/v1/orders/{id}/complete", requirePermission("orders:write", s.handleOrderComplete))
	mux.HandleFunc("POST /api/v1/orders/{id}/void", s.handleOrderVoid)
	mux.HandleFunc("POST /api/v1/orders/{id}/refund", s.handleOrderRefund)
	mux.HandleFunc("GET /api/v1/orders/{id}/returns", requirePermission("orders:read", s.handleOrderReturnHistory))
	mux.HandleFunc("GET /api/v1/orders/{id}/reconciliation", requirePermission("orders:read", s.handleOrderRefundReconciliation))
	mux.HandleFunc("POST /api/v1/orders/{id}/sync-recovery", s.handleSyncRecovery)
	mux.HandleFunc("GET /api/v1/orders/{id}/payments", requirePermission("orders:read", s.handlePaymentList))
	mux.HandleFunc("POST /api/v1/orders/{id}/payments", requirePermission("orders:write", s.handlePaymentCreate))
	mux.HandleFunc("GET /api/v1/orders/{id}/receipt", requirePermission("orders:read", s.handleOrderReceipt))
	mux.HandleFunc("GET /api/v1/receipts/{id}", requirePermission("orders:read", s.handleReceiptGet))
	mux.HandleFunc("GET /api/v1/inventory/balances/{product_id}", requirePermission("inventory:read", s.handleInventoryBalance))
	mux.HandleFunc("GET /api/v1/inventory/movements", requirePermission("inventory:read", s.handleInventoryMovements))

	s.httpServer = &http.Server{Addr: cfg.ListenAddress, Handler: requestIDMiddleware(securityHeadersMiddleware(s.localAuthMiddleware(mux))), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	return s
}

func (s *Server) Start() error { err := s.httpServer.ListenAndServe(); if errors.Is(err, http.ErrServerClosed) { return nil }; return err }
func (s *Server) Shutdown(ctx context.Context) error { return s.httpServer.Shutdown(ctx) }

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "shajretail-pos-service", "environment": s.cfg.Environment, "started_at": s.startedAt, "uptime_ms": time.Since(s.startedAt).Milliseconds()}) }
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) { ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second); defer cancel(); if err := s.db.Ping(ctx); err != nil { writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": "database_unavailable"}); return }; if _, err := s.device.Get(ctx); err != nil { writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": "device_identity_unavailable"}); return }; writeJSON(w, http.StatusOK, map[string]any{"status": "ready"}) }
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) { ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second); defer cancel(); snapshot, err := observability.New(s.db, outbox.New(s.db), s.cfg.BackupDirectory).Collect(ctx); if err != nil { writeError(w, http.StatusInternalServerError, "diagnostics_unavailable"); return }; writeJSON(w, http.StatusOK, snapshot) }
func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) { identity, err := s.device.Get(r.Context()); if err != nil { writeError(w, http.StatusInternalServerError, "device_identity_unavailable"); return }; writeJSON(w, http.StatusOK, identity) }
func (s *Server) handleDeviceRegistration(w http.ResponseWriter, r *http.Request) { var input device.Registration; dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)); dec.DisallowUnknownFields(); if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid_registration_payload"); return }; identity, err := s.device.ApplyRegistration(r.Context(), input); if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }; writeJSON(w, http.StatusOK, identity) }
func (s *Server) handleDeviceHeartbeat(w http.ResponseWriter, r *http.Request) { if err := s.device.RecordHeartbeat(r.Context()); err != nil { writeError(w, http.StatusInternalServerError, "heartbeat_failed"); return }; w.WriteHeader(http.StatusNoContent) }

func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) { limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); products, err := s.catalog.Search(r.Context(), r.URL.Query().Get("q"), s.currentStoreID(r.Context()), limit); if err != nil { writeError(w, http.StatusInternalServerError, "catalog_search_failed"); return }; writeJSON(w, http.StatusOK, map[string]any{"items": products, "count": len(products)}) }
func (s *Server) handleCatalogBarcode(w http.ResponseWriter, r *http.Request) { product, err := s.catalog.GetByBarcode(r.Context(), r.PathValue("barcode"), s.currentStoreID(r.Context())); if errors.Is(err, catalog.ErrNotFound) { writeError(w, http.StatusNotFound, "product_not_found"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "catalog_lookup_failed"); return }; writeJSON(w, http.StatusOK, product) }
func (s *Server) handleCatalogProduct(w http.ResponseWriter, r *http.Request) { product, err := s.catalog.GetProduct(r.Context(), strings.TrimSpace(r.PathValue("id")), s.currentStoreID(r.Context())); if errors.Is(err, catalog.ErrNotFound) { writeError(w, http.StatusNotFound, "product_not_found"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "catalog_lookup_failed"); return }; writeJSON(w, http.StatusOK, product) }
func (s *Server) handleCatalogCategories(w http.ResponseWriter, r *http.Request) { categories, err := s.catalog.ListCategories(r.Context()); if err != nil { writeError(w, http.StatusInternalServerError, "category_lookup_failed"); return }; writeJSON(w, http.StatusOK, map[string]any{"items": categories, "count": len(categories)}) }

func (s *Server) handleCustomerSearch(w http.ResponseWriter, r *http.Request) { limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); items, err := s.customers.Search(r.Context(), r.URL.Query().Get("q"), limit); if err != nil { writeError(w, http.StatusInternalServerError, "customer_search_failed"); return }; writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)}) }
func (s *Server) handleCustomerGet(w http.ResponseWriter, r *http.Request) { item, err := s.customers.Get(r.Context(), r.PathValue("id")); if errors.Is(err, customer.ErrNotFound) { writeError(w, http.StatusNotFound, "customer_not_found"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "customer_lookup_failed"); return }; writeJSON(w, http.StatusOK, item) }
func (s *Server) handleCustomerCreate(w http.ResponseWriter, r *http.Request) { var input customer.UpsertInput; dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)); dec.DisallowUnknownFields(); if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid_customer_payload"); return }; item, err := s.customers.Create(r.Context(), input); if err != nil { writeError(w, http.StatusBadRequest, normalizeCustomerError(err)); return }; writeJSON(w, http.StatusCreated, item) }
func (s *Server) handleCustomerUpdate(w http.ResponseWriter, r *http.Request) { var input customer.UpsertInput; dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)); dec.DisallowUnknownFields(); if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid_customer_payload"); return }; item, err := s.customers.Update(r.Context(), r.PathValue("id"), input); if errors.Is(err, customer.ErrNotFound) { writeError(w, http.StatusNotFound, "customer_not_found"); return }; if err != nil { writeError(w, http.StatusBadRequest, normalizeCustomerError(err)); return }; writeJSON(w, http.StatusOK, item) }

func (s *Server) handleOrderCreate(w http.ResponseWriter, r *http.Request) { var input orders.CreateInput; dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)); dec.DisallowUnknownFields(); if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid_order_payload"); return }; identity, err := s.device.Get(r.Context()); if err != nil || identity.StoreID == nil { writeError(w, http.StatusConflict, "device_not_registered_to_store"); return }; input.StoreID = *identity.StoreID; input.TerminalID = identity.TerminalID; order, err := s.orders.Create(r.Context(), input); if errors.Is(err, orders.ErrInvalidOrder) { writeError(w, http.StatusBadRequest, "invalid_order"); return }; if err != nil { writeError(w, http.StatusBadRequest, "order_create_failed"); return }; if err := s.recordOrderCreator(r.Context(), order.ID); err != nil { writeError(w, http.StatusInternalServerError, "order_audit_failed"); return }; if err := s.recordOrderApproval(r.Context(), order.ID); err != nil { writeError(w, http.StatusInternalServerError, "order_approval_audit_failed"); return }; writeJSON(w, http.StatusCreated, order) }
func (s *Server) handleOrderGet(w http.ResponseWriter, r *http.Request) { order, err := s.orders.Get(r.Context(), r.PathValue("id")); if errors.Is(err, orders.ErrNotFound) { writeError(w, http.StatusNotFound, "order_not_found"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "order_lookup_failed"); return }; writeJSON(w, http.StatusOK, order) }
func (s *Server) handleOrderList(w http.ResponseWriter, r *http.Request) { storeID := s.currentStoreID(r.Context()); if storeID == "" { writeError(w, http.StatusConflict, "device_not_registered_to_store"); return }; limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); offset, _ := strconv.Atoi(r.URL.Query().Get("offset")); if page, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && page > 1 { effectiveLimit := limit; if effectiveLimit <= 0 || effectiveLimit > 200 { effectiveLimit = 50 }; offset = (page - 1) * effectiveLimit }; items, err := s.orders.List(r.Context(), storeID, r.URL.Query().Get("status"), limit, offset); if err != nil { writeError(w, http.StatusInternalServerError, "order_list_failed"); return }; writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "limit": limit, "offset": offset}) }
func (s *Server) handleOrderComplete(w http.ResponseWriter, r *http.Request) { eventOutbox := outbox.New(s.db); auditHook := s.cashierCompletionAuditHook(r.Context()); order, err := s.orders.CompleteWith(r.Context(), r.PathValue("id"), s.inventory.ApplySaleTx, s.receipts.ApplyCompletionTx, eventOutbox.ApplySaleCompletedTx, auditHook); if errors.Is(err, orders.ErrNotFound) { writeError(w, http.StatusNotFound, "order_not_found"); return }; if errors.Is(err, orders.ErrAlreadyComplete) { writeError(w, http.StatusConflict, "order_already_complete"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "order_complete_failed"); return }; receipt, receiptErr := s.receipts.GetByOrder(r.Context(), order.ID); if receiptErr != nil { writeJSON(w, http.StatusOK, order); return }; writeJSON(w, http.StatusOK, map[string]any{"order": order, "receipt": receipt}) }

func (s *Server) handlePaymentCreate(w http.ResponseWriter, r *http.Request) { var input payments.CreateInput; dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)); dec.DisallowUnknownFields(); if err := dec.Decode(&input); err != nil { writeError(w, http.StatusBadRequest, "invalid_payment_payload"); return }; payment, summary, err := s.payments.Create(r.Context(), r.PathValue("id"), input); if errors.Is(err, payments.ErrOrderNotFound) { writeError(w, http.StatusNotFound, "order_not_found"); return }; if errors.Is(err, payments.ErrInvalidPayment) { writeError(w, http.StatusBadRequest, "invalid_payment"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "payment_create_failed"); return }; if err := s.recordPaymentCreator(r.Context(), payment.ID); err != nil { writeError(w, http.StatusInternalServerError, "payment_audit_failed"); return }; writeJSON(w, http.StatusCreated, map[string]any{"payment": payment, "summary": summary}) }
func (s *Server) handlePaymentList(w http.ResponseWriter, r *http.Request) { items, summary, err := s.payments.ListForOrder(r.Context(), r.PathValue("id")); if errors.Is(err, payments.ErrOrderNotFound) { writeError(w, http.StatusNotFound, "order_not_found"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "payment_list_failed"); return }; writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "summary": summary}) }
func (s *Server) handleOrderReceipt(w http.ResponseWriter, r *http.Request) { item, err := s.receipts.GetByOrder(r.Context(), r.PathValue("id")); if errors.Is(err, receipts.ErrNotFound) { writeError(w, http.StatusNotFound, "receipt_not_found"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "receipt_lookup_failed"); return }; writeJSON(w, http.StatusOK, item) }
func (s *Server) handleReceiptGet(w http.ResponseWriter, r *http.Request) { item, err := s.receipts.Get(r.Context(), r.PathValue("id")); if errors.Is(err, receipts.ErrNotFound) { writeError(w, http.StatusNotFound, "receipt_not_found"); return }; if err != nil { writeError(w, http.StatusInternalServerError, "receipt_lookup_failed"); return }; writeJSON(w, http.StatusOK, item) }
func (s *Server) handleInventoryBalance(w http.ResponseWriter, r *http.Request) { storeID := s.currentStoreID(r.Context()); if storeID == "" { writeError(w, http.StatusConflict, "device_not_registered_to_store"); return }; item, err := s.inventory.GetBalance(r.Context(), storeID, r.PathValue("product_id")); if err != nil { writeError(w, http.StatusInternalServerError, "inventory_balance_failed"); return }; writeJSON(w, http.StatusOK, item) }
func (s *Server) handleInventoryMovements(w http.ResponseWriter, r *http.Request) { storeID := s.currentStoreID(r.Context()); if storeID == "" { writeError(w, http.StatusConflict, "device_not_registered_to_store"); return }; limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); items, err := s.inventory.ListMovements(r.Context(), storeID, r.URL.Query().Get("product_id"), limit); if err != nil { writeError(w, http.StatusInternalServerError, "inventory_movements_failed"); return }; writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)}) }

func normalizeCustomerError(err error) string { switch err.Error() { case "customer_name_required", "invalid_credit_limit", "invalid_currency": return err.Error(); default: return "customer_write_failed" } }
func (s *Server) currentStoreID(ctx context.Context) string { identity, err := s.device.Get(ctx); if err != nil || identity.StoreID == nil { return "" }; return *identity.StoreID }
func writeJSON(w http.ResponseWriter, status int, payload any) { w.Header().Set("Content-Type", "application/json"); w.WriteHeader(status); _ = json.NewEncoder(w).Encode(payload) }
func writeError(w http.ResponseWriter, status int, code string) { writeJSON(w, status, map[string]any{"error": code}) }
func securityHeadersMiddleware(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Content-Type-Options", "nosniff"); w.Header().Set("Cache-Control", "no-store"); next.ServeHTTP(w, r) }) }
func requestIDMiddleware(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requestID := r.Header.Get("X-Request-ID"); if requestID == "" { requestID = time.Now().UTC().Format("20060102T150405.000000000") }; w.Header().Set("X-Request-ID", requestID); next.ServeHTTP(w, r) }) }
