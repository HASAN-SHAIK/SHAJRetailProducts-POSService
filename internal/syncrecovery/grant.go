package syncrecovery

import (
    "crypto"
    "crypto/rsa"
    "crypto/sha256"
    "crypto/x509"
    "encoding/base64"
    "encoding/json"
    "encoding/pem"
    "errors"
    "strings"
    "time"
)

var ErrInvalidGrant = errors.New("invalid POS sync recovery grant")

type Grant struct {
    RecoveryID string `json:"recovery_id"`
    TenantID string `json:"tenant_id"`
    DeviceID string `json:"device_id"`
    OrderID string `json:"order_id"`
    OrderingKey string `json:"ordering_key"`
    EventID string `json:"event_id"`
    ApprovedByUserID string `json:"approved_by_user_id"`
    Reason string `json:"reason"`
    Exp int64 `json:"exp"`
}

type claims struct {
    Type string `json:"type"`
    RecoveryID string `json:"recovery_id"`
    TenantID string `json:"tenant_id"`
    DeviceID string `json:"device_id"`
    OrderID string `json:"order_id"`
    OrderingKey string `json:"ordering_key"`
    EventID string `json:"event_id"`
    ApprovedByUserID string `json:"approved_by_user_id"`
    Reason string `json:"reason"`
    Exp int64 `json:"exp"`
    Iss string `json:"iss"`
    Aud any `json:"aud"`
}

func Verify(token, publicKeyPEM string) (Grant, error) {
    parts := strings.Split(strings.TrimSpace(token), ".")
    if len(parts) != 3 || strings.TrimSpace(publicKeyPEM) == "" { return Grant{}, ErrInvalidGrant }
    headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
    if err != nil { return Grant{}, ErrInvalidGrant }
    var header map[string]any
    if json.Unmarshal(headerBytes, &header) != nil || header["alg"] != "RS256" { return Grant{}, ErrInvalidGrant }

    block, _ := pem.Decode([]byte(strings.TrimSpace(publicKeyPEM)))
    if block == nil { return Grant{}, ErrInvalidGrant }
    parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
    if err != nil { return Grant{}, ErrInvalidGrant }
    key, ok := parsed.(*rsa.PublicKey)
    if !ok { return Grant{}, ErrInvalidGrant }

    signature, err := base64.RawURLEncoding.DecodeString(parts[2])
    if err != nil { return Grant{}, ErrInvalidGrant }
    digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
    if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil { return Grant{}, ErrInvalidGrant }

    payload, err := base64.RawURLEncoding.DecodeString(parts[1])
    if err != nil { return Grant{}, ErrInvalidGrant }
    var c claims
    if json.Unmarshal(payload, &c) != nil { return Grant{}, ErrInvalidGrant }
    if c.Type != "pos_sync_recovery_grant" || c.Iss != "shajtech-central" || !audienceContains(c.Aud, "shajtech-pos-edge") || c.Exp <= time.Now().UTC().Unix() || c.RecoveryID == "" || c.TenantID == "" || c.DeviceID == "" || c.OrderID == "" || c.OrderingKey != "sales_order:"+c.OrderID || c.EventID == "" || c.ApprovedByUserID == "" || strings.TrimSpace(c.Reason) == "" {
        return Grant{}, ErrInvalidGrant
    }
    return Grant{RecoveryID:c.RecoveryID,TenantID:c.TenantID,DeviceID:c.DeviceID,OrderID:c.OrderID,OrderingKey:c.OrderingKey,EventID:c.EventID,ApprovedByUserID:c.ApprovedByUserID,Reason:c.Reason,Exp:c.Exp}, nil
}

func audienceContains(v any, expected string) bool {
    switch t := v.(type) {
    case string:
        return t == expected
    case []any:
        for _, item := range t { if s, ok := item.(string); ok && s == expected { return true } }
    }
    return false
}
