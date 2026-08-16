# SHAJRetailProducts V1 Authentication / Authorization Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is authoritative for tenant identity, interactive authentication, refresh/session lifecycle, platform-admin identity, role/permission grants, branch/store scope, POS-device eligibility, offline-grant issuance/revocation policy, and recovery authority.
- POSService/SQLite may verify a Central-signed offline grant, bind it to the installed POS identity, enroll a local PIN credential, create short-lived local sessions, and enforce the granted permission/store scope while offline. POS must not invent tenant users, permissions, branch scope, or Central device authority.
- Frontend may collect credentials and render/guard routes/actions from authoritative session/permission state, but must not be the security boundary for protected Central or POS mutations.

## Acceptance matrix

| Capability | Existing implementation / evidence | Status | V1 closure requirement |
|---|---|---|---|
| Tenant login + access JWT | Backend #61 aligns tenant authentication, admin provisioning, persisted PostgreSQL role constraints and fresh-tenant provisioning with the authoritative `admin`/`manager`/`cashier`/transitional-`staff` permission catalog; exact-head V1 Auth acceptance + control-plane CI passed | CERTIFIED | frozen except real defects; downstream protected-action permission enforcement remains tracked separately |
| Refresh-token lifecycle | Central uses persisted refresh-token service, HttpOnly refresh cookie and refresh rotation/revocation paths | PARTIAL | make rotation replay-safe under concurrent use; certify expiry, logout/revocation and tenant isolation |
| Platform-admin authentication | Separate admin cookie/token verifier requires `type=admin`, `admin_id` and `platform_admin` role | PARTIAL | certify key separation, expiry, route isolation and rejection of tenant tokens |
| Role -> permission authority | Backend #61 proves every supported V1 tenant role can be provisioned/authenticated from the same Central permission catalog | PARTIAL | audit protected Central/POS actions and certify permissions are enforced server-side rather than only exposed in claims/UI |
| Branch/store authorization | Tenant token carries branch/all-branch/store permissions; certified Store/Device branch authority and guards already exist | PARTIAL | certify restricted users cannot cross branch/store scope while all-branch admins retain intended interactive access |
| POS offline grant issuance | Backend #62 resolves the requested POS through active Central registration, rejects revoked/unregistered devices and restricted-user branch mismatch, and narrows the signed offline grant to the trusted device branch | CERTIFIED | frozen except real defects |
| Offline grant cryptography | POS verifies RS256 signature, issuer, audience, expiry, grant type and required claims | PARTIAL | certify invalid signature/algorithm/issuer/audience/expiry/key cases fail closed and key rotation policy is explicit |
| Offline grant device binding | Central #62 binds issuance to the active registered device/branch; POS `EnrollForDevice` verifies grant `device_id` equals the installed POS and enforces store match for restricted users | PARTIAL | certify copied grants cannot enroll on another POS and branch/store mismatch fails closed end-to-end |
| Local PIN enrollment | POS derives PBKDF2-SHA256 PIN hashes with random salt and persists only derived credentials plus Central grant facts | PARTIAL | certify PIN policy, re-enrollment replacement semantics, no plaintext persistence and expired grant rejection |
| Local login/session lifecycle | POS local auth tracks failed attempts/lockout, random hashed session tokens, session TTL bounded by grant expiry and logout | PARTIAL | certify lockout, expiry, logout, restart behavior and disabled-user/session rejection |
| Offline permission enforcement | Central grant carries permission/store snapshots; POS local user/session exposes them | PARTIAL | trace protected POS handlers and certify sale/discount/void/refund/admin actions enforce granted permissions rather than Frontend role checks |
| Online permission enforcement | Central has admin/tenant middleware plus route-level guards/permission helpers | PARTIAL | audit protected V1 routes and close any write path that relies only on Frontend visibility or broad staff access |
| Authentication/device revocation interaction | Store/Device V1 certifies revoked devices lose new sync/config authority while retained offline config remains local; #62 blocks new offline grants to revoked devices | PARTIAL | define/certify disabled user/tenant/device and permission changes versus already-issued offline grants and local sessions |
| Frontend authentication UX | Existing Frontend login/session/route guard flows are present | PARTIAL | certify login/logout/expiry, unauthorized/forbidden handling, permission-based action visibility and offline local-login UX without treating UI guards as authority |
| Secrets/cookie/token hardening | JWT secrets, offline grant private/public key and HttpOnly cookies exist; secure cookie enabled in production | PARTIAL | certify required-secret fail-closed startup/config, SameSite/secure behavior, no token leakage, dependency/key review and production-secret documentation |
| Auth diagnostics/audit | Central/POS already have broader diagnostics/audit infrastructure | GAP | expose actionable read-only auth/session/grant/device failures without credentials/secrets and preserve security-relevant audit evidence |
| Cross-tenant isolation | Tenant DB resolution is derived from verified tenant context and platform admin auth is separate | PARTIAL | executable cross-tenant token/session/grant tests proving no tenant/customer/store/device authority crosses tenant boundaries |

## Closed gaps in this phase

1. **Supported-role mismatch — CLOSED:** Backend #61 now uses the authoritative Central permission catalog consistently in tenant auth and tenant-user provisioning and migrates PostgreSQL role constraints for manager/cashier support.
2. **Offline-grant device authority — CLOSED:** Backend #62 requires an active registered Central POS device, derives the trusted branch from that registration, rejects branch mismatch for restricted users, and narrows all offline grants to one POS branch.

## Next proven gap

**Refresh replay/concurrency:** Central currently finds a valid refresh token and later revokes/inserts its successor in separate operations. Concurrent refresh requests can therefore observe the same predecessor as valid before either revocation commits. V1 must make refresh consumption/rotation atomic and prove only one successor can be issued.

## Ordered V1 work

1. Make refresh rotation atomic/replay-safe; certify expiry, logout and tenant isolation.
2. Certify platform-admin token isolation and key/route separation.
3. Certify POS RS256 grant verification, device/store binding, PIN/session lockout/expiry and offline permission enforcement.
4. Complete server-side Central permission + branch authorization audit.
5. Close disabled user/tenant/device + already-issued offline grant/session semantics.
6. Certify Frontend authentication/offline-login UX and actionable 401/403 handling.
7. Complete secret/key/cookie/dependency review, auth diagnostics/audit and cross-tenant isolation.
8. Run final Authentication / Authorization V1 cross-repository release acceptance, then freeze except for real defects.

## Release decision

Authentication / Authorization V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, all privileged decisions are enforced outside the Frontend, Central remains identity/permission/recovery authority, POS offline authorization is limited to valid Central-signed grants bound to the installed device/store scope, and no unresolved critical authentication or authorization defect remains.
