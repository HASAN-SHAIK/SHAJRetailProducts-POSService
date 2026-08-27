package device

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

type Identity struct {
	DeviceID        string  `json:"device_id"`
	InstallationID  string  `json:"installation_id"`
	StoreID         *string `json:"store_id,omitempty"`
	StoreNumber     *string `json:"store_number,omitempty"`
	POSNo           *string `json:"pos_no,omitempty"`
	TouchpointID    *string `json:"touchpoint_id,omitempty"`
	// TerminalID is retained as a compatibility alias while callers migrate to pos_no.
	TerminalID      *string `json:"terminal_id,omitempty"`
	Status          string  `json:"status"`
	RegisteredAt    *string `json:"registered_at,omitempty"`
	LastHeartbeatAt *string `json:"last_heartbeat_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type Registration struct {
	StoreID      string `json:"store_id"`
	StoreNumber  string `json:"store_number"`
	POSNo        string `json:"pos_no"`
	TouchpointID string `json:"touchpoint_id"`
	// TerminalID accepts legacy clients. POSNo wins when both are supplied.
	TerminalID string `json:"terminal_id,omitempty"`
}

type Service struct{ db *database.DB }

func New(db *database.DB) *Service { return &Service{db: db} }

type InstallationSeed struct {
	DeviceID       string
	InstallationID string
}

func (s *Service) EnsureInstallation(ctx context.Context) (Identity, error) {
	return s.EnsureInstallationWithSeed(ctx, InstallationSeed{})
}

func (s *Service) EnsureInstallationWithSeed(ctx context.Context, seed InstallationSeed) (Identity, error) {
	identity, err := s.Get(ctx)
	if err == nil {
		if seed.DeviceID != "" || seed.InstallationID != "" {
			return s.ApplyInstallationSeed(ctx, seed)
		}
		return identity, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Identity{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	deviceID := strings.TrimSpace(seed.DeviceID)
	if deviceID == "" {
		deviceID, err = randomID("dev", 16)
		if err != nil {
			return Identity{}, err
		}
	}
	installationID := strings.TrimSpace(seed.InstallationID)
	if installationID == "" {
		installationID, err = randomID("install", 24)
		if err != nil {
			return Identity{}, err
		}
	}

	_, err = s.db.SQL().ExecContext(ctx, `
        INSERT INTO device_identity(singleton_id, device_id, installation_id, status, created_at, updated_at)
        VALUES(1, ?, ?, 'unregistered', ?, ?)
        ON CONFLICT(singleton_id) DO NOTHING`, deviceID, installationID, now, now)
	if err != nil {
		return Identity{}, fmt.Errorf("create device identity: %w", err)
	}
	return s.Get(ctx)
}

func (s *Service) ApplyInstallationSeed(ctx context.Context, seed InstallationSeed) (Identity, error) {
	if seed.DeviceID == "" && seed.InstallationID == "" {
		return s.Get(ctx)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.SQL().ExecContext(ctx, `
        UPDATE device_identity
        SET device_id = CASE WHEN ? <> '' THEN ? ELSE device_id END,
            installation_id = CASE WHEN ? <> '' THEN ? ELSE installation_id END,
            updated_at = ?
        WHERE singleton_id = 1 AND status <> 'revoked'`,
		seed.DeviceID, seed.DeviceID, seed.InstallationID, seed.InstallationID, now)
	if err != nil {
		return Identity{}, fmt.Errorf("apply seeded device identity: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return Identity{}, errors.New("device identity is not available for seeded identity")
	}
	return s.Get(ctx)
}

func (s *Service) Get(ctx context.Context) (Identity, error) {
	var out Identity
	var storeID, storeNumber, posNo, touchpointID, terminalID, registeredAt, heartbeat sql.NullString
	err := s.db.SQL().QueryRowContext(ctx, `
        SELECT device_id, installation_id, store_id, store_number, pos_no, touchpoint_id,
               terminal_id, status, registered_at, last_heartbeat_at, created_at, updated_at
        FROM device_identity WHERE singleton_id = 1`).Scan(
		&out.DeviceID, &out.InstallationID, &storeID, &storeNumber, &posNo, &touchpointID,
		&terminalID, &out.Status, &registeredAt, &heartbeat, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return Identity{}, err
	}
	out.StoreID = nullableString(storeID)
	out.StoreNumber = nullableString(storeNumber)
	out.POSNo = nullableString(posNo)
	out.TouchpointID = nullableString(touchpointID)
	out.TerminalID = nullableString(terminalID)
	out.RegisteredAt = nullableString(registeredAt)
	out.LastHeartbeatAt = nullableString(heartbeat)
	return out, nil
}

func (s *Service) ApplyRegistration(ctx context.Context, registration Registration) (Identity, error) {
	storeID := strings.TrimSpace(registration.StoreID)
	storeNumber := strings.TrimSpace(registration.StoreNumber)
	posNo := strings.TrimSpace(registration.POSNo)
	if posNo == "" {
		posNo = strings.TrimSpace(registration.TerminalID)
	}
	touchpointID := strings.TrimSpace(registration.TouchpointID)
	if storeID == "" || storeNumber == "" || posNo == "" || touchpointID == "" {
		return Identity{}, errors.New("store_id, store_number, pos_no and touchpoint_id are required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.SQL().ExecContext(ctx, `
        UPDATE device_identity
        SET store_id = ?, store_number = ?, pos_no = ?, touchpoint_id = ?, terminal_id = ?,
            status = 'active', registered_at = COALESCE(registered_at, ?), updated_at = ?
        WHERE singleton_id = 1 AND status <> 'revoked'`,
		storeID, storeNumber, posNo, touchpointID, posNo, now, now)
	if err != nil {
		return Identity{}, fmt.Errorf("apply device registration: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return Identity{}, errors.New("device identity is not available for registration")
	}
	return s.Get(ctx)
}

func (s *Service) RecordHeartbeat(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.SQL().ExecContext(ctx, `UPDATE device_identity SET last_heartbeat_at = ?, updated_at = ? WHERE singleton_id = 1`, now, now)
	return err
}

func randomID(prefix string, bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}
