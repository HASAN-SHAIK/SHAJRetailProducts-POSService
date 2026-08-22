package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type syncEventDiagnostics struct {
	CollectedAt     time.Time                      `json:"collected_at"`
	Limit           int                            `json:"limit"`
	Outbox          []outboxEventDetails           `json:"outbox"`
	Inbox           []inboxEventDetails            `json:"inbox"`
	EffectiveConfig effectiveConfigSyncDiagnostics `json:"effective_config"`
	LocalAuth       localAuthDiagnostics           `json:"local_auth"`
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
	StuckSince       string          `json:"stuck_since,omitempty"`
	AgeSeconds       int64           `json:"age_seconds,omitempty"`
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
	StuckSince    string          `json:"stuck_since,omitempty"`
	AgeSeconds    int64           `json:"age_seconds,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type skipDiagnosticInput struct {
	Reason string `json:"reason"`
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
		item.StuckSince, item.AgeSeconds = syncStuckEvidence(item.Status, item.CreatedAt, item.AvailableAt, item.LockedAt, item.PublishedAt)
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
		item.StuckSince, item.AgeSeconds = syncStuckEvidence(item.Status, item.ReceivedAt, "", "", item.AppliedAt)
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

func (s *Server) handleSkipOutboxDiagnostic(w http.ResponseWriter, r *http.Request) {
	user, ok := localUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "local_session_required")
		return
	}
	if !canSkipSyncDiagnostic(user) {
		writeError(w, http.StatusForbidden, "permission_denied")
		return
	}
	input := readSkipDiagnosticInput(w, r)
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		writeError(w, http.StatusBadRequest, "outbox_event_id_required")
		return
	}
	result, err := s.skipOutboxDiagnostic(r.Context(), eventID, user.UserID, input.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "outbox_event_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "outbox_skip_failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSkipInboxDiagnostic(w http.ResponseWriter, r *http.Request) {
	user, ok := localUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "local_session_required")
		return
	}
	if !canSkipSyncDiagnostic(user) {
		writeError(w, http.StatusForbidden, "permission_denied")
		return
	}
	input := readSkipDiagnosticInput(w, r)
	messageID := strings.TrimSpace(r.PathValue("id"))
	if messageID == "" {
		writeError(w, http.StatusBadRequest, "inbox_message_id_required")
		return
	}
	result, err := s.skipInboxDiagnostic(r.Context(), messageID, user.UserID, input.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "inbox_message_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "inbox_skip_failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func readSkipDiagnosticInput(w http.ResponseWriter, r *http.Request) skipDiagnosticInput {
	if r.Body == nil {
		return skipDiagnosticInput{}
	}
	var input skipDiagnosticInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	_ = dec.Decode(&input)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		input.Reason = "Skipped from Sync Center"
	}
	if len(input.Reason) > 240 {
		input.Reason = input.Reason[:240]
	}
	return input
}

func canSkipSyncDiagnostic(user LocalUserContext) bool {
	if hasLocalPermission(user, "*") || hasLocalPermission(user, "sync:skip") {
		return true
	}
	role := strings.ToLower(strings.TrimSpace(user.Role))
	return role == "admin" || role == "manager"
}

func (s *Server) skipOutboxDiagnostic(ctx context.Context, eventID, userID, reason string) (map[string]any, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lastError := fmt.Sprintf("skipped_by_user:%s reason:%s", strings.TrimSpace(userID), strings.TrimSpace(reason))
	res, err := s.db.SQL().ExecContext(ctx, `
		UPDATE outbox_events
		SET status='published',
		    published_at=?,
		    locked_at=NULL,
		    lock_owner=NULL,
		    last_error=?,
		    metadata_json=json_set(metadata_json,'$.sync_skipped',json_object('skipped_at',?,'skipped_by',?,'reason',?))
		WHERE id=? AND status IN ('pending','processing','failed','dead_letter')`,
		now, truncateDiagnosticNote(lastError), now, strings.TrimSpace(userID), strings.TrimSpace(reason), eventID)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"queue": "outbox", "id": eventID, "status": "published", "skipped": true, "skipped_at": now}, nil
}

func (s *Server) skipInboxDiagnostic(ctx context.Context, messageID, userID, reason string) (map[string]any, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lastError := fmt.Sprintf("skipped_by_user:%s reason:%s", strings.TrimSpace(userID), strings.TrimSpace(reason))
	res, err := s.db.SQL().ExecContext(ctx, `
		UPDATE inbox_messages
		SET status='applied',
		    applied_at=?,
		    last_error=?
		WHERE message_id=? AND status IN ('received','processing','failed')`,
		now, truncateDiagnosticNote(lastError), messageID)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"queue": "inbox", "id": messageID, "status": "applied", "skipped": true, "skipped_at": now}, nil
}

func truncateDiagnosticNote(value string) string {
	if len(value) <= 1000 {
		return value
	}
	return value[:1000]
}

func syncStuckEvidence(status, createdAt, availableAt, lockedAt, completedAt string) (string, int64) {
	if completedAt != "" {
		return "", 0
	}
	candidate := createdAt
	if status == "processing" && lockedAt != "" {
		candidate = lockedAt
	} else if availableAt != "" {
		candidate = availableAt
	}
	parsed, err := time.Parse(time.RFC3339Nano, candidate)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, candidate)
	}
	if err != nil {
		return candidate, 0
	}
	age := time.Since(parsed).Seconds()
	if age < 0 {
		age = 0
	}
	return parsed.UTC().Format(time.RFC3339Nano), int64(age)
}
