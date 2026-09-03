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

type authRuntimeResponse struct {
	Status int
	Body   []byte
}

func TestV1LocalAuthRuntimeHTTPEnrollLoginProtectedRouteLogout(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, filepath.Join(t.TempDir(), "pos.db"))
	defer db.Close()

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

	privateKey, publicPEM := authRuntimeTestKeyPair(t)
	app := newTestServer(db, deviceService)
	app.cfg.OfflineGrantSecret = publicPEM
	app.cfg.CentralTenantID = "tenant-1"
	app.localAuth = localauth.New(db, publicPEM)
	baseURL := startAuthLiveRuntime(t, app)

	grant := authRuntimeSignedGrant(t, privateKey, map[string]any{
		"type":              "pos_offline_grant",
		"user_id":           "cashier-1",
		"tenant_id":         "tenant-1",
		"role":              "cashier",
		"device_id":         registered.DeviceID,
		"branch_id":         "store-1",
		"all_branch_access": false,
		"permissions":       []string{"products:read", "orders:read", "pos:sale"},
		"grant_id":          "grant-runtime-1",
		"iss":               "shajtech-central",
		"aud":               "shajtech-pos-edge",
		"exp":               time.Now().Add(time.Hour).Unix(),
	})

	enroll := authRuntimeRequest(t, baseURL, http.MethodPost, "/api/v1/auth/enroll", map[string]any{
		"offline_grant": grant,
		"pin":           "2468",
	}, "", "")
	if enroll.Status != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", enroll.Status, enroll.Body)
	}

	badLogin := authRuntimeRequest(t, baseURL, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"user_id": "cashier-1",
		"pin":     "1111",
	}, "", "")
	if badLogin.Status != http.StatusUnauthorized {
		t.Fatalf("bad login status=%d body=%s", badLogin.Status, badLogin.Body)
	}

	login := authRuntimeRequest(t, baseURL, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"user_id": "cashier-1",
		"pin":     "2468",
	}, "", "")
	if login.Status != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Status, login.Body)
	}
	var loginBody struct {
		SessionToken string `json:"session_token"`
		User         struct {
			UserID      string   `json:"user_id"`
			TenantID    string   `json:"tenant_id"`
			BranchID    string   `json:"branch_id"`
			Role        string   `json:"role"`
			Permissions []string `json:"permissions"`
		} `json:"user"`
	}
	if err := json.Unmarshal(login.Body, &loginBody); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(loginBody.SessionToken) == "" {
		t.Fatal("login returned empty session token")
	}
	if loginBody.User.UserID != "cashier-1" || loginBody.User.TenantID != "tenant-1" || loginBody.User.BranchID != "store-1" || loginBody.User.Role != "cashier" {
		t.Fatalf("unexpected login user: %#v", loginBody.User)
	}

	withoutSession := authRuntimeRequest(t, baseURL, http.MethodGet, "/api/v1/catalog/products?q=milk", nil, "machine-token", "")
	if withoutSession.Status != http.StatusUnauthorized {
		t.Fatalf("protected route without session status=%d body=%s", withoutSession.Status, withoutSession.Body)
	}

	withSession := authRuntimeRequest(t, baseURL, http.MethodGet, "/api/v1/catalog/products?q=milk", nil, "machine-token", loginBody.SessionToken)
	if withSession.Status != http.StatusOK {
		t.Fatalf("protected route with valid session status=%d body=%s", withSession.Status, withSession.Body)
	}

	logout := authRuntimeRequest(t, baseURL, http.MethodPost, "/api/v1/auth/logout", nil, "machine-token", loginBody.SessionToken)
	if logout.Status != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Status, logout.Body)
	}

	afterLogout := authRuntimeRequest(t, baseURL, http.MethodGet, "/api/v1/catalog/products?q=milk", nil, "machine-token", loginBody.SessionToken)
	if afterLogout.Status != http.StatusUnauthorized {
		t.Fatalf("logged-out session still authorized status=%d body=%s", afterLogout.Status, afterLogout.Body)
	}
}

func startAuthLiveRuntime(t *testing.T, app *Server) string {
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
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(shutdownCtx)
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("auth runtime shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("auth runtime did not stop")
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return "http://" + addr
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("auth POSService runtime did not become healthy: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func authRuntimeRequest(t *testing.T, baseURL, method, path string, body any, machineToken, sessionToken string) authRuntimeResponse {
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
	return authRuntimeResponse{Status: resp.StatusCode, Body: raw}
}

func authRuntimeTestKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
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

func authRuntimeSignedGrant(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
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
