package security

import (
    "crypto/rand"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/hex"
    "errors"
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "strings"
)

const HeaderLocalToken = "X-POS-Local-Token"

type LocalAuth struct {
    tokenHash      [32]byte
    allowedOrigins map[string]struct{}
}

func LoadOrCreate(deviceID, configuredToken, tokenFile string, allowedOrigins []string) (*LocalAuth, error) {
    if strings.TrimSpace(deviceID) == "" {
        return nil, errors.New("device id is required for local auth")
    }
    token := strings.TrimSpace(configuredToken)
    if token == "" {
        var err error
        token, err = loadOrCreateToken(deviceID, tokenFile)
        if err != nil {
            return nil, err
        }
    }
    if len(token) < 32 {
        return nil, errors.New("local API token must be at least 32 characters")
    }

    origins := make(map[string]struct{}, len(allowedOrigins))
    for _, origin := range allowedOrigins {
        origin = strings.TrimRight(strings.TrimSpace(origin), "/")
        if origin != "" {
            origins[origin] = struct{}{}
        }
    }
    return &LocalAuth{tokenHash: sha256.Sum256([]byte(token)), allowedOrigins: origins}, nil
}

func (a *LocalAuth) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
        if origin != "" {
            if _, ok := a.allowedOrigins[origin]; !ok {
                writeSecurityError(w, http.StatusForbidden, "origin_not_allowed")
                return
            }
            w.Header().Set("Access-Control-Allow-Origin", origin)
            w.Header().Set("Vary", "Origin")
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, "+HeaderLocalToken+", X-POS-Session-Token, X-POS-Approval-Token")
            w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
            w.Header().Set("Access-Control-Max-Age", "600")
        }

        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusNoContent)
            return
        }
        if isPublicPath(r.URL.Path) {
            next.ServeHTTP(w, r)
            return
        }

        presented := r.Header.Get(HeaderLocalToken)
        actualHash := sha256.Sum256([]byte(presented))
        if presented == "" || subtle.ConstantTimeCompare(a.tokenHash[:], actualHash[:]) != 1 {
            writeSecurityError(w, http.StatusUnauthorized, "local_auth_required")
            return
        }
        next.ServeHTTP(w, r)
    })
}

func isPublicPath(path string) bool {
    return path == "/api/v1/health" || path == "/api/v1/ready"
}

func loadOrCreateToken(deviceID, tokenFile string) (string, error) {
    if strings.TrimSpace(tokenFile) == "" {
        return "", errors.New("local token file path is required")
    }
    if raw, err := os.ReadFile(tokenFile); err == nil {
        token := strings.TrimSpace(string(raw))
        if len(token) < 32 {
            return "", errors.New("local API token file contains an invalid token")
        }
        return token, nil
    } else if !errors.Is(err, os.ErrNotExist) {
        return "", fmt.Errorf("read local token file: %w", err)
    }

    if err := os.MkdirAll(filepath.Dir(tokenFile), 0o750); err != nil {
        return "", fmt.Errorf("create local token directory: %w", err)
    }
    random := make([]byte, 32)
    if _, err := rand.Read(random); err != nil {
        return "", fmt.Errorf("generate local API token: %w", err)
    }
    token := deviceID + "." + hex.EncodeToString(random)
    if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
        return "", fmt.Errorf("persist local API token: %w", err)
    }
    return token, nil
}

func writeSecurityError(w http.ResponseWriter, status int, code string) {
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Cache-Control", "no-store")
    w.WriteHeader(status)
    _, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}
