package localauth

import (
    "context"
    "crypto"
    "crypto/hmac"
    "crypto/rand"
    "crypto/rsa"
    "crypto/sha256"
    "crypto/x509"
    "database/sql"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "encoding/pem"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

var (
    ErrInvalidGrant = errors.New("invalid offline grant")
    ErrInvalidPIN = errors.New("invalid pin")
    ErrUserNotFound = errors.New("local user not found")
    ErrSessionInvalid = errors.New("local session invalid")
    ErrLocked = errors.New("local user temporarily locked")
)

const (
    pinIterations = 120000
    pinKeyLen = 32
    defaultSessionTTL = 12 * time.Hour
    maxFailedAttempts = 5
    failedAttemptLockout = 5 * time.Minute
)

type User struct {
    UserID string `json:"user_id"`
    TenantID string `json:"tenant_id"`
    Role string `json:"role"`
    BranchID string `json:"branch_id,omitempty"`
    AllBranchAccess bool `json:"all_branch_access"`
    Permissions []string `json:"permissions"`
    GrantID string `json:"grant_id"`
    GrantExpiresAt time.Time `json:"grant_expires_at"`
}

type grantClaims struct {
    Type string `json:"type"`
    UserID string `json:"user_id"`
    TenantID string `json:"tenant_id"`
    Role string `json:"role"`
    BranchID string `json:"branch_id"`
    AllBranchAccess bool `json:"all_branch_access"`
    Permissions []string `json:"permissions"`
    GrantID string `json:"grant_id"`
    Exp int64 `json:"exp"`
    Iss string `json:"iss"`
    Aud any `json:"aud"`
}

type Service struct {
    db *database.DB
    grantPublicKey []byte
    sessionTTL time.Duration
}

func New(db *database.DB, grantPublicKey string) *Service {
    return &Service{db: db, grantPublicKey: []byte(strings.TrimSpace(grantPublicKey)), sessionTTL: defaultSessionTTL}
}

func (s *Service) Enroll(ctx context.Context, grant, pin string) (User, error) {
    if !validPIN(pin) { return User{}, ErrInvalidPIN }
    claims, err := s.verifyGrant(grant)
    if err != nil { return User{}, err }

    salt := make([]byte, 16)
    if _, err := rand.Read(salt); err != nil { return User{}, fmt.Errorf("generate pin salt: %w", err) }
    hash := pbkdf2SHA256([]byte(pin), salt, pinIterations, pinKeyLen)
    permissionsJSON, _ := json.Marshal(claims.Permissions)
    now := time.Now().UTC()
    expires := time.Unix(claims.Exp, 0).UTC()

    _, err = s.db.SQL().ExecContext(ctx, `
        INSERT INTO local_users(user_id, tenant_id, role, branch_id, all_branch_access, permissions_json,
            pin_salt, pin_hash, pin_iterations, failed_attempts, locked_until, grant_id, grant_expires_at, enabled, updated_at)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(user_id) DO UPDATE SET
            tenant_id=excluded.tenant_id,
            role=excluded.role,
            branch_id=excluded.branch_id,
            all_branch_access=excluded.all_branch_access,
            permissions_json=excluded.permissions_json,
            pin_salt=excluded.pin_salt,
            pin_hash=excluded.pin_hash,
            pin_iterations=excluded.pin_iterations,
            failed_attempts=0,
            locked_until=NULL,
            grant_id=excluded.grant_id,
            grant_expires_at=excluded.grant_expires_at,
            enabled=1,
            updated_at=excluded.updated_at`,
        claims.UserID, claims.TenantID, claims.Role, nullable(claims.BranchID), boolInt(claims.AllBranchAccess), string(permissionsJSON),
        salt, hash, pinIterations, 0, nil, claims.GrantID, expires.Format(time.RFC3339Nano), 1, now.Format(time.RFC3339Nano))
    if err != nil { return User{}, fmt.Errorf("enroll local user: %w", err) }
    _, _ = s.db.SQL().ExecContext(ctx, `DELETE FROM local_auth_sessions WHERE user_id = ?`, claims.UserID)
    return claims.user(), nil
}

func (s *Service) Login(ctx context.Context, userID, pin string) (string, User, error) {
    userID = strings.TrimSpace(userID)
    if userID == "" || !validPIN(pin) { return "", User{}, ErrInvalidPIN }

    var u User
    var branch, lockedUntil sql.NullString
    var permissionsJSON string
    var salt, expected []byte
    var iterations, failedAttempts int
    var allBranch, enabled int
    var grantExpires string
    err := s.db.SQL().QueryRowContext(ctx, `
        SELECT user_id, tenant_id, role, branch_id, all_branch_access, permissions_json,
               pin_salt, pin_hash, pin_iterations, failed_attempts, locked_until,
               grant_id, grant_expires_at, enabled
        FROM local_users WHERE user_id = ?`, userID).Scan(
        &u.UserID, &u.TenantID, &u.Role, &branch, &allBranch, &permissionsJSON,
        &salt, &expected, &iterations, &failedAttempts, &lockedUntil,
        &u.GrantID, &grantExpires, &enabled)
    if errors.Is(err, sql.ErrNoRows) { return "", User{}, ErrUserNotFound }
    if err != nil { return "", User{}, fmt.Errorf("read local user: %w", err) }
    if enabled != 1 { return "", User{}, ErrUserNotFound }

    now := time.Now().UTC()
    if lockedUntil.Valid {
        if until, err := time.Parse(time.RFC3339Nano, lockedUntil.String); err == nil && now.Before(until) {
            return "", User{}, ErrLocked
        }
    }
    expires, err := time.Parse(time.RFC3339Nano, grantExpires)
    if err != nil || !now.Before(expires) { return "", User{}, ErrInvalidGrant }

    actual := pbkdf2SHA256([]byte(pin), salt, iterations, len(expected))
    if !hmac.Equal(actual, expected) {
        failedAttempts++
        if failedAttempts >= maxFailedAttempts {
            until := now.Add(failedAttemptLockout).Format(time.RFC3339Nano)
            _, _ = s.db.SQL().ExecContext(ctx, `UPDATE local_users SET failed_attempts = 0, locked_until = ?, updated_at = ? WHERE user_id = ?`, until, now.Format(time.RFC3339Nano), userID)
        } else {
            _, _ = s.db.SQL().ExecContext(ctx, `UPDATE local_users SET failed_attempts = ?, updated_at = ? WHERE user_id = ?`, failedAttempts, now.Format(time.RFC3339Nano), userID)
        }
        return "", User{}, ErrInvalidPIN
    }
    _, _ = s.db.SQL().ExecContext(ctx, `UPDATE local_users SET failed_attempts = 0, locked_until = NULL, updated_at = ? WHERE user_id = ?`, now.Format(time.RFC3339Nano), userID)

    if branch.Valid { u.BranchID = branch.String }
    u.AllBranchAccess = allBranch == 1
    u.GrantExpiresAt = expires
    _ = json.Unmarshal([]byte(permissionsJSON), &u.Permissions)

    raw := make([]byte, 32)
    if _, err := rand.Read(raw); err != nil { return "", User{}, fmt.Errorf("generate local session: %w", err) }
    token := base64.RawURLEncoding.EncodeToString(raw)
    tokenHash := sha256.Sum256([]byte(token))
    sessionExpires := now.Add(s.sessionTTL)
    if expires.Before(sessionExpires) { sessionExpires = expires }
    _, err = s.db.SQL().ExecContext(ctx, `
        INSERT INTO local_auth_sessions(token_hash, user_id, created_at, expires_at, last_seen_at)
        VALUES(?,?,?,?,?)`, tokenHash[:], u.UserID, now.Format(time.RFC3339Nano), sessionExpires.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
    if err != nil { return "", User{}, fmt.Errorf("create local session: %w", err) }
    return token, u, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
    token = strings.TrimSpace(token)
    if token == "" { return User{}, ErrSessionInvalid }
    tokenHash := sha256.Sum256([]byte(token))
    var u User
    var branch sql.NullString
    var permissionsJSON string
    var allBranch int
    var sessionExpires, grantExpires string
    err := s.db.SQL().QueryRowContext(ctx, `
        SELECT u.user_id, u.tenant_id, u.role, u.branch_id, u.all_branch_access, u.permissions_json,
               u.grant_id, u.grant_expires_at, s.expires_at
        FROM local_auth_sessions s
        JOIN local_users u ON u.user_id = s.user_id
        WHERE s.token_hash = ? AND u.enabled = 1`, tokenHash[:]).Scan(
        &u.UserID, &u.TenantID, &u.Role, &branch, &allBranch, &permissionsJSON,
        &u.GrantID, &grantExpires, &sessionExpires)
    if errors.Is(err, sql.ErrNoRows) { return User{}, ErrSessionInvalid }
    if err != nil { return User{}, fmt.Errorf("read local session: %w", err) }
    now := time.Now().UTC()
    sExp, err1 := time.Parse(time.RFC3339Nano, sessionExpires)
    gExp, err2 := time.Parse(time.RFC3339Nano, grantExpires)
    if err1 != nil || err2 != nil || !now.Before(sExp) || !now.Before(gExp) {
        _, _ = s.db.SQL().ExecContext(ctx, `DELETE FROM local_auth_sessions WHERE token_hash = ?`, tokenHash[:])
        return User{}, ErrSessionInvalid
    }
    if branch.Valid { u.BranchID = branch.String }
    u.AllBranchAccess = allBranch == 1
    u.GrantExpiresAt = gExp
    _ = json.Unmarshal([]byte(permissionsJSON), &u.Permissions)
    _, _ = s.db.SQL().ExecContext(ctx, `UPDATE local_auth_sessions SET last_seen_at = ? WHERE token_hash = ?`, now.Format(time.RFC3339Nano), tokenHash[:])
    return u, nil
}

func (s *Service) Logout(ctx context.Context, token string) {
    tokenHash := sha256.Sum256([]byte(strings.TrimSpace(token)))
    _, _ = s.db.SQL().ExecContext(ctx, `DELETE FROM local_auth_sessions WHERE token_hash = ?`, tokenHash[:])
}

func (s *Service) verifyGrant(token string) (grantClaims, error) {
    if len(s.grantPublicKey) == 0 { return grantClaims{}, ErrInvalidGrant }
    parts := strings.Split(token, ".")
    if len(parts) != 3 { return grantClaims{}, ErrInvalidGrant }

    headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
    if err != nil { return grantClaims{}, ErrInvalidGrant }
    var header map[string]any
    if json.Unmarshal(headerBytes, &header) != nil || header["alg"] != "RS256" { return grantClaims{}, ErrInvalidGrant }

    block, _ := pem.Decode(s.grantPublicKey)
    if block == nil { return grantClaims{}, ErrInvalidGrant }
    var publicKey *rsa.PublicKey
    if parsed, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
        key, ok := parsed.(*rsa.PublicKey)
        if !ok { return grantClaims{}, ErrInvalidGrant }
        publicKey = key
    } else if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
        publicKey = key
    } else {
        return grantClaims{}, ErrInvalidGrant
    }

    signature, err := base64.RawURLEncoding.DecodeString(parts[2])
    if err != nil { return grantClaims{}, ErrInvalidGrant }
    signingInput := parts[0] + "." + parts[1]
    digest := sha256.Sum256([]byte(signingInput))
    if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
        return grantClaims{}, ErrInvalidGrant
    }

    payload, err := base64.RawURLEncoding.DecodeString(parts[1])
    if err != nil { return grantClaims{}, ErrInvalidGrant }
    var claims grantClaims
    if json.Unmarshal(payload, &claims) != nil { return grantClaims{}, ErrInvalidGrant }
    now := time.Now().UTC().Unix()
    if claims.Type != "pos_offline_grant" || claims.Iss != "shajtech-central" || !audienceContains(claims.Aud, "shajtech-pos-edge") || claims.Exp <= now || claims.UserID == "" || claims.TenantID == "" || claims.Role == "" || claims.GrantID == "" {
        return grantClaims{}, ErrInvalidGrant
    }
    return claims, nil
}

func (c grantClaims) user() User {
    return User{UserID: c.UserID, TenantID: c.TenantID, Role: c.Role, BranchID: c.BranchID, AllBranchAccess: c.AllBranchAccess, Permissions: c.Permissions, GrantID: c.GrantID, GrantExpiresAt: time.Unix(c.Exp, 0).UTC()}
}

func validPIN(pin string) bool {
    if len(pin) < 4 || len(pin) > 8 { return false }
    for _, r := range pin { if r < '0' || r > '9' { return false } }
    return true
}
func boolInt(v bool) int { if v { return 1 }; return 0 }
func nullable(v string) any { if strings.TrimSpace(v) == "" { return nil }; return strings.TrimSpace(v) }
func audienceContains(v any, expected string) bool {
    switch t := v.(type) {
    case string:
        return t == expected
    case []any:
        for _, item := range t { if s, ok := item.(string); ok && s == expected { return true } }
    }
    return false
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
    hLen := 32
    blocks := (keyLen + hLen - 1) / hLen
    out := make([]byte, 0, blocks*hLen)
    for block := 1; block <= blocks; block++ {
        mac := hmac.New(sha256.New, password)
        _, _ = mac.Write(salt)
        _, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
        u := mac.Sum(nil)
        t := append([]byte(nil), u...)
        for i := 1; i < iterations; i++ {
            mac = hmac.New(sha256.New, password)
            _, _ = mac.Write(u)
            u = mac.Sum(nil)
            for j := range t { t[j] ^= u[j] }
        }
        out = append(out, t...)
    }
    return out[:keyLen]
}

func TokenFingerprint(token string) string {
    sum := sha256.Sum256([]byte(token))
    return hex.EncodeToString(sum[:8])
}
