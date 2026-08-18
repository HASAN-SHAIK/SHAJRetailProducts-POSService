package localauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
)

type deviceBindingClaims struct {
	DeviceID string `json:"device_id"`
}

// EnrollForDevice verifies the Central signature first, then binds enrollment
// to this installation. A copied grant cannot be enrolled on another POS.
// Store-scoped users must also match the store registered to this POS; users
// with all-branch access remain device-bound but may enroll at any store.
// When expectedTenantID is supplied by the packaged runtime, the Central grant
// must also belong to that configured tenant so a grant from another tenant
// cannot be enrolled even when a physical device identifier is reused there.
func (s *Service) EnrollForDevice(ctx context.Context, grant, pin, deviceID, storeID string, expectedTenantID ...string) (User, error) {
	claims, err := s.verifyGrant(grant)
	if err != nil {
		return User{}, err
	}

	parts := strings.Split(grant, ".")
	if len(parts) != 3 {
		return User{}, ErrInvalidGrant
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return User{}, ErrInvalidGrant
	}
	var binding deviceBindingClaims
	if json.Unmarshal(payload, &binding) != nil {
		return User{}, ErrInvalidGrant
	}

	expectedDevice := strings.TrimSpace(deviceID)
	if expectedDevice == "" || strings.TrimSpace(binding.DeviceID) != expectedDevice {
		return User{}, ErrInvalidGrant
	}
	if len(expectedTenantID) > 0 {
		expectedTenant := strings.TrimSpace(expectedTenantID[0])
		if expectedTenant == "" || strings.TrimSpace(claims.TenantID) != expectedTenant {
			return User{}, ErrInvalidGrant
		}
	}
	expectedStore := strings.TrimSpace(storeID)
	if expectedStore != "" && !claims.AllBranchAccess && strings.TrimSpace(claims.BranchID) != expectedStore {
		return User{}, ErrInvalidGrant
	}

	return s.Enroll(ctx, grant, pin)
}
