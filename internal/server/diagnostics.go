package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

type syncEventDiagnostics struct {
	CollectedAt     time.Time                      `json:"collected_at"`
	Limit           int                            `json:"limit"`
	Outbox          []outboxEventDetails           `json:"outbox"`
	Inbox           []inboxEventDetails            `json:"inbox"`
	EffectiveConfig effectiveConfigSyncDiagnostics `json:"effective_config"`
	LocalAuth       localAuthDiagnostics            `json:"local_auth"`
}

type effectiveConfigSyncDiagnostics struct {
	LastAttemptAt string `json:"last_attempt_at,omitempty"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	LastETag      string `json:"last_etag,omitempty"`
}

type outboxEventDetails struct {
	ID               string          `json:"id"`
	AggregateType    string          `json:"aggregate_type"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int             `json:"aggregate_version"`
	EventType        string          `json:"event_type"`
	SchemaVersion    int             `json:"schema_version"`
	OrderingKey      string          `json:"ordering_key"`
	Status           string          `json:"status"`
	AttemptCount     int             `json:"attempt_count"`
	AvailableAt      string          `json:"available_at"`
	LockedAt         string          `json:"locked_at,omitempty"`
	LockOwner        string          `json:"lock_owner,omitempty"`
	LastError        string          `json:"last_error,omitempty"`
	CreatedAt        string          `json:"created_at"`
	PublishedAt      string          `json:"published_at,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	Metadata         json.RawMessage `json:"metadata"`
}

type inboxEventDetails struct {
	MessageID     string          `json:"message_id"`
	MessageType   string          `json:"message_type"`
	SchemaVersion int             `json:"schema_version"`
	Source        string          `json:"source"`
	Status        string          `json:"status"`
	AttemptCount  int             `json:"attempt_count"`
	ReceivedAt    string          `json:"received_at"`
	AppliedAt     string          `json:"applied_at,omitempty"`
	LastError     string          `json:"last_error,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

func (s *Server) handleSyncEventDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	outboxItems, err := s.loadOutboxDiagnostics(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "outbox_diagnostics_unavailable")
		return
	}
	inboxItems, err := s.loadInboxDiagnostics(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "inbox_diagnostics_unavailable")
		return
	}
	effectiveConfig, err := s.loadEffectiveConfigDiagnostics(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "effective_config_diagnostics_unavailable")
		return
	}
	localAuth, err := s.loadLocalAuthDiagnostics(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "local_auth_diagnostics_unavailable")
		return
	}

	writeJSON(w, http.StatusOK, syncEventDiagnostics{
		CollectedAt:     time.Now().UTC(),
		Limit:           limit,
		Outbox:          outboxItems,
		Inbox:           inboxItems,
		EffectiveConfig: effectiveConfig,
		LocalAuth:       localAuth,
	})
}

func (s *Server) loadEffectiveConfigDiagnostics(ctx context.Context) (effectiveConfigSyncDiagnostics, error) {
	var attempt, success, lastError, etag sql.NullString
	err := s.db.SQL().QueryRowContext(ctx, `
		SELECT last_attempt_at,last_success_at,last_error,last_etag
		FROM effective_config_sync_state
		WHERE singleton_id=1`).Scan(&attempt, &success, &lastError, &etag)
	if errors.Is(err, sql.ErrNoRows) {
		return effectiveConfigSyncDiagnostics{}, nil
	}
	if err != nil {
		return effectiveConfigSyncDiagnostics{}, err
	}
	return effectiveConfigSyncDiagnostics{
		LastAttemptAt: nullableString(attempt),
		LastSuccessAt: nullableString(success),
		LastError:     nullableString(lastError),
		LastETag:      nullableString(etag),
	}, nil
}

func (s *Server) loadOutboxDiagnostics(ctx context.Context, limit int) ([]outboxEventDetails, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,ordering_key,
		       payload_json,metadata_json,status,attempt_count,available_at,locked_at,lock_owner,last_error,created_at,published_at
		FROM outbox_events
		WHERE status IN ('pending','processing','failed','dead_letter')
		ORDER BY
			CASE status WHEN 'dead_letter' THEN 0 WHEN 'failed' THEN 1 WHEN 'processing' THEN 2 ELSE 3 END,
			created_at,
			id
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]outboxEventDetails, 0)
	for rows.Next() {
		var item outboxEventDetails
		var payload string
		var metadata string
		var lockedAt, lockOwner, lastError, publishedAt sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.AggregateType,
			&item.AggregateID,
			&item.AggregateVersion,
			&item.EventType,
			&item.SchemaVersion,
			&item.OrderingKey,
			&payload,
			&metadata,
			&item.Status,
			&item.AttemptCount,
			&item.AvailableAt,
			&lockedAt,
			&lockOwner,
			&lastError,
			&item.CreatedAt,
			&publishedAt,
		); err != nil {
			return nil, err
		}
		item.Payload = rawJSON(payload)
		item.Metadata = rawJSON(metadata)
		item.LockedAt = nullableString(lockedAt)
		item.LockOwner = nullableString(lockOwner)
		item.LastError = nullableString(lastError)
		item.PublishedAt = nullableString(publishedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) loadInboxDiagnostics(ctx context.Context, limit int) ([]inboxEventDetails, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT message_id,message_type,schema_version,source,payload_json,status,attempt_count,received_at,applied_at,last_error
		FROM inbox_messages
		WHERE status IN ('received','processing','failed')
		ORDER BY
			CASE status WHEN 'failed' THEN 0 WHEN 'processing' THEN 1 ELSE 2 END,
			received_at,
			message_id
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]inboxEventDetails, 0)
	for rows.Next() {
		var item inboxEventDetails
		var payload string
		var appliedAt, lastError sql.NullString
		if err := rows.Scan(
			&item.MessageID,
			&item.MessageType,
			&item.SchemaVersion,
			&item.Source,
			&payload,
			&item.Status,
			&item.AttemptCount,
			&item.ReceivedAt,
			&appliedAt,
			&lastError,
		); err != nil {
			return nil, err
		}
		item.Payload = rawJSON(payload)
		item.AppliedAt = nullableString(appliedAt)
		item.LastError = nullableString(lastError)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func nullableString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func rawJSON(value string) json.RawMessage {
	if json.Valid([]byte(value)) {
		return json.RawMessage(value)
	}
	encoded, _ := json.Marshal(value)
	return json.RawMessage(encoded)
}
