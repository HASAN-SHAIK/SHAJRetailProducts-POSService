package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/effectiveconfig"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/observability"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/outbox"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/syncengine"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	response := map[string]any{"status": "ready", "database": "healthy"}
	if err := s.db.Ping(ctx); err != nil {
		response["status"] = "degraded"
		response["database"] = "unavailable"
	}
	if identity, err := s.device.Get(ctx); err == nil {
		response["device"] = identity
	}

	store := effectiveconfig.NewStore(s.db)
	state, _ := store.State(ctx)
	response["configuration_sync"] = state
	if snapshot, err := store.Get(ctx); err == nil {
		response["configuration"] = map[string]any{
			"available":    true,
			"etag":         snapshot.ETag,
			"fetched_at":   snapshot.FetchedAt,
			"generated_at": snapshot.GeneratedAt,
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		response["configuration"] = map[string]any{"available": false}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	snapshot, err := effectiveconfig.NewStore(s.db).Get(r.Context())
	if errors.Is(err, sql.ErrNoRows) {
		state, _ := effectiveconfig.NewStore(s.db).State(r.Context())
		writeJSON(w, http.StatusNotFound, map[string]any{"code": "config_snapshot_missing", "sync": state})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_snapshot_failed")
		return
	}
	w.Header().Set("ETag", `"`+snapshot.ETag+`"`)
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleRefreshConfig(w http.ResponseWriter, r *http.Request) {
	service, err := s.effectiveConfigService(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.SyncRequestTimeout+2*time.Second)
	defer cancel()
	changed, err := service.Refresh(ctx)
	if err != nil {
		state, _ := service.State(ctx)
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": "config_refresh_failed", "message": err.Error(), "sync": state})
		return
	}
	snapshot, err := service.Snapshot(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_snapshot_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": changed, "snapshot": snapshot})
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	snapshot, err := observability.New(s.db, outbox.New(s.db), s.cfg.BackupDirectory).Collect(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sync_status_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"database_ok": snapshot.DatabaseOK,
		"outbox":      snapshot.Outbox,
		"inbox": map[string]any{
			"received": snapshot.InboxReceived,
			"failed":   snapshot.InboxFailed,
		},
		"last_change_cursor": snapshot.LastChangeCursor,
	})
}

func (s *Server) handleSyncNow(w http.ResponseWriter, r *http.Request) {
	engine, err := s.syncEngine(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.SyncRequestTimeout+10*time.Second)
	defer cancel()
	published, err := engine.DispatchReady(ctx, 100)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": "sync_failed", "message": err.Error(), "published": published})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"published": published})
}

func (s *Server) effectiveConfigService(ctx context.Context) (*effectiveconfig.Service, error) {
	identity, err := s.device.Get(ctx)
	if err != nil {
		return nil, errors.New("device_identity_unavailable")
	}
	client, err := effectiveconfig.NewClient(s.cfg.CentralAPIURL, s.cfg.CentralTenantID, identity.DeviceID, s.cfg.CentralSyncToken, s.cfg.SyncRequestTimeout)
	if err != nil {
		return nil, errors.New("central_config_disabled")
	}
	return effectiveconfig.NewService(effectiveconfig.NewStore(s.db), client, nil, time.Minute), nil
}

func (s *Server) syncEngine(ctx context.Context) (*syncengine.Engine, error) {
	identity, err := s.device.Get(ctx)
	if err != nil {
		return nil, errors.New("device_identity_unavailable")
	}
	engine, err := syncengine.New(outbox.New(s.db), s.cfg.CentralAPIURL, s.cfg.CentralTenantID, s.cfg.CentralSyncToken, identity.DeviceID, s.cfg.SyncRequestTimeout, s.cfg.SyncPollInterval)
	if err != nil {
		return nil, errors.New("sync_not_configured")
	}
	if engine == nil {
		return nil, errors.New("sync_not_configured")
	}
	return engine, nil
}
