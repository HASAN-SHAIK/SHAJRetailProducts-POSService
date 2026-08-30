package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/device"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/localauth"
)

type sessionRestartHTTPResponse struct {
	Status int
	Body   []byte
}

func TestV1LocalSessionSurvivesLivePOSServiceRestartAndLogoutPersists(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pos.db")

	privateKey, publicPEM := sessionRestartKeyPair(t)

	db := openMigratedDB(t, dbPath)
	deviceService := device.New(db)
	identity, err := deviceService.EnsureInstallation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := deviceService.ApplyRegistration(ctx, device.Registration{
		StoreID:      "store-1",
		StoreNumber:  "STORE-001",
		POSNo:        "POS-01",
		TouchpointID: "TP-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if identity.DeviceID != registered.DeviceID {
		t.Fatalf("device identity changed during registration: %s != %s", identity.DeviceID, registered.DeviceID)
	}

	app1 := newTestServer(db, deviceService)
	app1.cfg.OfflineGrantSecret = publicPEM
	app1.cfg.CentralTenantID = "tenant-1"
	app1.localAuth = localauth.New(db, publicPEM)
	baseURL1, stop1 := startSessionRestartRuntime(t, app1)

	grant := sessionRestartSignedGrant(t, privateKey, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "cashier-restart",
		"tenant_id":         "tenant-1",
		"role":              "cashier",
		"device_id":         registered.DeviceID,
		"branch_id":         "store-1",
		"all_branch_access": false,
		"permissions":       []string{"products:read", "orders:read", "pos:sale"},
		"grant_id":          "grant-session-restart-1",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(time.Hour).Unix(),
	})

	enroll := sessionRestartRequest(t, baseURL1, http.MethodPost, "/api/v1/auth/enroll", map[string]any{
		"offline_grant": grant,
		"pin":           "2468",
	}, "", "")
	if enroll.Status != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", enroll.Status, enroll.Body)
	}

	login := sessionRestartRequest(t, baseURL1, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"user_id": "cashier-restart",
		"pin":     "2468",
	}, "", "")
	if login.Status != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Status, login.Body)
	}
	var loginBody struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(login.Body, &loginBody); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if strings.TrimSpace(loginBody.SessionToken) == "" {
		t.Fatal("login returned empty session token")
	}

	beforeRestart := sessionRestartRequest(t, baseURL1, http.MethodGet, "/api/v1/catalog/products?q=milk", nil, "machine-token", loginBody.SessionToken)
	if beforeRestart.Status != http.StatusOK {
		t.Fatalf("pre-restart protected request status=%d body=%s", beforeRestart.Status, beforeRestart.Body)
	}

	var sessionsBefore int
	var lastSeenBefore string
	if err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(last_seen_at)
		FROM local_auth_sessions
		WHERE user_id = ?`, "cashier-restart").Scan(&sessionsBefore, &lastSeenBefore); err != nil {
		t.Fatalf("read pre-restart session state: %v", err)
	}
	if sessionsBefore != 1 || strings.TrimSpace(lastSeenBefore) == "" {
		t.Fatalf("unexpected pre-restart session state: count=%d last_seen=%q", sessionsBefore, lastSeenBefore)
	}

	stop1()
	if err := db.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	reopened := openMigratedDB(t, dbPath)
	deviceService2 := device.New(reopened)
	persistedIdentity, err := deviceService2.Get(ctx)
	if err != nil {
		t.Fatalf("read device after restart: %v", err)
	}
	if persistedIdentity.DeviceID != registered.DeviceID || persistedIdentity.StoreID != "store-1" {
		t.Fatalf("device identity did not survive restart: %+v", persistedIdentity)
	}

	app2 := newTestServer(reopened, deviceService2)
	app2.cfg.OfflineGrantSecret = publicPEM
	app2.cfg.CentralTenantID = "tenant-1"
	app2.localAuth = localauth.New(reopened, publicPEM)
	baseURL2, stop2 := startSessionRestartRuntime(t, app2)

	afterRestart := sessionRestartRequest(t, baseURL2, http.MethodGet, "/api/v1/catalog/products?q=milk", nil, "machine-token", loginBody.SessionToken)
	if afterRestart.Status != http.StatusOK {
		t.Fatalf("persisted session rejected after live restart status=%d body=%s", afterRestart.Status, afterRestart.Body)
	}

	var sessionsAfter int
	var lastSeenAfter string
	if err := reopened.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(last_seen_at)
		FROM local_auth_sessions
		WHERE user_id = ?`, "cashier-restart").Scan(&sessionsAfter, &lastSeenAfter); err != nil {
		t.Fatalf("read post-restart session state: %v", err)
	}
	if sessionsAfter != 1 {
		t.Fatalf("restart duplicated or lost persisted session: count=%d", sessionsAfter)
	}
	beforeTime, err := time.Parse(time.RFC3339Nano, lastSeenBefore)
	if err != nil {
		t.Fatalf("parse pre-restart last_seen_at: %v", err)
	}
	afterTime, err := time.Parse(time.RFC3339Nano, lastSeenAfter)
	if err != nil {
		t.Fatalf("parse post-restart last_seen_at: %v", err)
	}
	if afterTime.Before(beforeTime) {
		t.Fatalf("post-restart authentication moved last_seen_at backwards: before=%s after=%s", lastSeenBefore, lastSeenAfter)
	}

	logout := sessionRestartRequest(t, baseURL2, http.MethodPost, "/api/v1/auth/logout", nil, "machine-token", loginBody.SessionToken)
	if logout.Status != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Status, logout.Body)
	}
	var remaining int
	if err := reopened.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM local_auth_sessions WHERE user_id = ?`, "cashier-restart").Scan(&remaining); err != nil {
		t.Fatalf("read session count after logout: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("logout did not durably delete session: count=%d", remaining)
	}

	stop2()
	if err := reopened.Close(); err != nil {
		t.Fatalf("close second database: %v", err)
	}

	reopenedAgain := openMigratedDB(t, dbPath)
	defer reopenedAgain.Close()
	deviceService3 := device.New(reopenedAgain)
	app3 := newTestServer(reopenedAgain, deviceService3)
	app3.cfg.OfflineGrantSecret = publicPEM
	app3.cfg.CentralTenantID = "tenant-1"
	app3.localAuth = localauth.New(reopenedAgain, publicPEM)
	baseURL3, stop3 := startSessionRestartRuntime(t, app3)
	defer stop3()

	afterLogoutRestart := sessionRestartRequest(t, baseURL3, http.MethodGet, "/api/v1/catalog/products?q=milk", nil, "machine-token", loginBody.SessionToken)
	if afterLogoutRestart.Status != http.StatusUnauthorized {
		t.Fatalf("logged-out session resurrected after second restart status=%d body=%s", afterLogoutRestart.Status, afterLogoutRestart.Body)
	}
	if err := reopenedAgain.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM local_auth_sessions WHERE user_id = ?`, "cashier-restart").Scan(&remaining); err != nil {
		t.Fatalf("read final session count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("logged-out session reappeared after restart: count=%d", remaining)
	}
}

func startSessionRestartRuntime(t *testing.T, app *Server) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	app.cfg.ListenAddress = addr
	app.httpServer.Addr = addr
	serverErr := make(chan error, 1)
	go func() { serverErr <- app.Start() }()

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("POSService restart runtime did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			t.Fatalf("shutdown POSService restart runtime: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Fatalf("POSService restart runtime stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("POSService restart runtime did not stop")
		}
	}
	return "http://" + addr, stop
}

func sessionRestartRequest(t *testing.T, baseURL, method, path string, body any, machineToken, sessionToken string) sessionRestartHTTPResponse {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if machineToken != "" {
		req.Header.Set("X-POS-Local-Token", machineToken)
	}
	if sessionToken != "" {
		req.Header.Set("X-POS-Session-Token", sessionToken)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("runtime request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return sessionRestartHTTPResponse{Status: resp.StatusCode, Body: raw}
}

func sessionRestartKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	return key, string(publicPEM)
}

func sessionRestartSignedGrant(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := encodedHeader + "." + encodedPayload
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}
