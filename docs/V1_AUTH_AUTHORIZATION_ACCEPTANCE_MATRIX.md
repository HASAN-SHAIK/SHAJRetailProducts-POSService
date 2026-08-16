# SHAJRetailProducts V1 Authentication / Authorization Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is authoritative for tenant identity, interactive authentication, refresh/session lifecycle, platform-admin identity, role/permission grants, branch/store scope, POS-device eligibility, offline-grant issuance/revocation policy, and recovery authority.
- POSService/SQLite may verify a Central-signed offline grant, bind it to the installed POS identity, enroll a local PIN credential, create short-lived local sessions, and enforce the granted permission/store scope while offline. POS must not invent tenant users, permissions, branch scope, or Central device authority.
- Frontend may collect credentials and render/guard routes/actions from authoritative session/permission state, but must not be the security boundary for protected Central or POS mutations.

## Acceptance matrix

| Capability | Existing implementation / evidence | Status | V1 closure requirement |
|---|---|---|---|
| Tenant login + access JWT | Central `authController` authenticates tenant users and signs typed tenant JWTs; `authTenantMiddleware` verifies signature, tenant/user identity, active tenant state and role | PARTIAL | certify supported V1 roles and fail-closed tenant/user claims; fix any mismatch between login roles and route middleware |
| Refresh-token lifecycle | Central uses persisted refresh-token service, HttpOnly refresh cookie and refresh rotation/revocation paths | PARTIAL | certify rotation, expiry, logout/revocation, replay rejection and tenant isolation |
| Platform-admin authentication | Separate admin cookie/token verifier requires `type=admin`, `admin_id` and `platform_admin` role | PARTIAL | certify key separation, expiry, route isolation and rejection of tenant tokens |
| Role -> permission authority | Central `rolePermissions` defines admin/manager/cashier/staff permissions including POS sale/discount/void/refund/approve | PARTIAL | certify each supported V1 role is accepted by tenant auth and protected actions enforce permissions server-side |
| Branch/store authorization | Tenant token carries branch/all-branch/store permissions; certified Store/Device branch authority and guards already exist | PARTIAL | certify restricted users cannot cross branch/store scope while all-branch admins retain intended access |
| POS offline grant issuance | Central `/api/auth/offline-grant` signs RS256 grants containing tenant/user/role/branch/device/permissions | GAP | require the requested POS device to be an active Central registration and enforce user/device branch compatibility before signing |
| Offline grant cryptography | POS verifies RS256 signature, issuer, audience, expiry, grant type and required claims | PARTIAL | certify invalid signature/algorithm/issuer/audience/expiry/key cases fail closed and key rotation policy is explicit |
| Offline grant device binding | POS `EnrollForDevice` verifies grant `device_id` equals the installed POS and enforces store match for restricted users | PARTIAL | certify copied grants cannot enroll on another POS and branch/store mismatch fails closed |
| Local PIN enrollment | POS derives PBKDF2-SHA256 PIN hashes with random salt and persists only derived credentials plus Central grant facts | PARTIAL | certify PIN policy, re-enrollment replacement semantics, no plaintext persistence and expired grant rejection |
| Local login/session lifecycle | POS local auth tracks failed attempts/lockout, random hashed session tokens, session TTL bounded by grant expiry and logout | PARTIAL | certify lockout, expiry, logout, restart behavior and disabled-user/session rejection |
| Offline permission enforcement | Central grant carries permission/store snapshots; POS local user/session exposes them | PARTIAL | trace protected POS handlers and certify sale/discount/void/refund/admin actions enforce granted permissions rather than Frontend role checks |
| Online permission enforcement | Central has admin/tenant middleware plus route-level guards/permission helpers | PARTIAL | audit protected V1 routes and close any write path that relies only on Frontend visibility or broad staff access |
| Authentication/device revocation interaction | Store/Device V1 certifies revoked devices lose new sync/config authority while retained offline config remains local | PARTIAL | define/certify how revoked device, disabled tenant/user, expired grant and refreshed permissions affect continued offline authentication |
| Frontend authentication UX | Existing Frontend login/session/route guard flows are present | PARTIAL | certify login/logout/expiry, unauthorized/forbidden handling, permission-based action visibility and offline local-login UX without treating UI guards as authority |
| Secrets/cookie/token hardening | JWT secrets, offline grant private/public key and HttpOnly cookies exist; secure cookie enabled in production | PARTIAL | certify required-secret fail-closed startup/config, SameSite/secure behavior, no token leakage, dependency/key review and production-secret documentation |
| Auth diagnostics/audit | Central/POS already have broader diagnostics/audit infrastructure | GAP | expose actionable read-only auth/session/grant/device failures without credentials/secrets and preserve security-relevant audit evidence |
| Cross-tenant isolation | Tenant DB resolution is derived from verified tenant context and platform admin auth is separate | PARTIAL | executable cross-tenant token/session/grant tests proving no tenant/customer/store/device authority crosses tenant boundaries |

## First proven gaps

1. **Supported-role mismatch:** Central permission definitions include `manager` and `cashier`, but current tenant authentication middleware only accepts `admin` and `staff`; V1 must reconcile this before claiming role enforcement.
2. **Offline-grant device authority:** `/api/auth/offline-grant` accepts a caller-supplied `device_id` and signs it without first proving that device is an active Central registration for the tenant/branch. POS later binds the signed grant to that device, so Central issuance must close this authority gap.

## Ordered V1 work

1. Certify/fix supported tenant roles and server-side permission enforcement.
2. Bind Central offline-grant issuance to active registered device + allowed branch/store scope.
3. Certify access/refresh/logout/replay and platform-admin token isolation.
4. Certify POS RS256 grant verification, device/store binding, PIN/session lockout/expiry and offline permission enforcement.
5. Close disabled user/tenant/device + offline grant revocation/expiry semantics.
6. Certify Frontend authentication/offline-login UX and actionable 401/403 handling.
7. Complete secret/key/cookie/dependency review, auth diagnostics/audit and cross-tenant isolation.
8. Run final Authentication / Authorization V1 cross-repository release acceptance, then freeze except for real defects.

## Release decision

Authentication / Authorization V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, all privileged decisions are enforced outside the Frontend, Central remains identity/permission/recovery authority, POS offline authorization is limited to valid Central-signed grants bound to the installed device/store scope, and no unresolved critical authentication or authorization defect remains.
