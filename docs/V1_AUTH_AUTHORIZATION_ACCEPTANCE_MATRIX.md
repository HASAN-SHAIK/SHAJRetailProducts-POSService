# SHAJRetailProducts V1 Authentication / Authorization Acceptance Matrix

Status: **RELEASE CERTIFIED — FROZEN EXCEPT REAL DEFECTS**

## Authority boundary

- Central Backend/PostgreSQL is authoritative for tenant identity, interactive authentication, refresh/session lifecycle, platform-admin identity, role/permission grants, branch/store scope, POS-device eligibility, offline-grant issuance/revocation policy, and recovery authority.
- POSService/SQLite verifies Central-signed offline grants, binds them to the installed POS/device/store/tenant identity, stores derived PIN credentials, creates bounded local sessions, and enforces the granted permissions while offline. POS does not invent Central users, roles, permissions, tenant identity, branch scope, or device authority.
- Frontend is non-authoritative. Online Central sessions use credentialed HttpOnly cookies; disconnected protected routes require a POSService-validated local session.

## Acceptance matrix

| Capability | Certified evidence | Status |
|---|---|---|
| Tenant login/current tenant authority | Backend #61/#71 | CERTIFIED |
| Refresh rotation/replay/concurrency | Backend #63 real PostgreSQL acceptance | CERTIFIED |
| Platform-admin key isolation | Backend #64 | CERTIFIED |
| Role -> permission authority | Backend #61/#66/#68/#69/#72/#73; POS #207/#209 | CERTIFIED |
| Branch/store authorization | Backend #66 | CERTIFIED |
| POS offline-grant issuance | Backend #62/#67 | CERTIFIED |
| Offline-grant cryptography | POS #207 | CERTIFIED |
| Offline grant device/store/tenant binding | Backend #62; POS #207/#212 | CERTIFIED |
| Local PIN enrollment | POS #207 | CERTIFIED |
| Local login/session lifecycle | POS #207 | CERTIFIED |
| Offline permission enforcement | POS #209 | CERTIFIED |
| Online permission enforcement | Backend #65/#66/#68/#69/#72/#73 | CERTIFIED |
| Authentication/device revocation interaction | Backend #67/#71 plus bounded signed-grant/session expiry policy | CERTIFIED |
| Frontend authentication UX/offline boundary | Frontend #41 | CERTIFIED |
| Browser token/cookie hardening | Frontend #42 + Backend #75 | CERTIFIED |
| Tenant/admin signing-key separation and fail-closed tenant key | Backend #64/#75 | CERTIFIED |
| POS local machine API trust boundary | POS #214 | CERTIFIED |
| Credential-safe Auth diagnostics | POS #215 | CERTIFIED |
| Cross-tenant online/refresh/grant isolation | Backend #74 + POS #212 | CERTIFIED |

## Security decisions frozen for V1

1. Central interactive browser authentication is cookie-only: access and refresh credentials are HttpOnly; Frontend does not persist or send a JavaScript-readable access JWT.
2. Tenant and platform-admin JWT authorities use separate required secrets. Offline grants use the separate Central RS256 offline-grant key boundary.
3. Refresh tokens rotate atomically and cannot be replayed to create multiple successors.
4. Offline grants are minted from current Central user authority and an active registered POS device, then bound again at POS to configured tenant/device/store identity.
5. Disconnected revocation is bounded by the signed grant expiry and local session expiry; V1 does not claim instantaneous revocation while physically disconnected.
6. All privileged V1 decisions are enforced outside the Frontend. Existing manager-approval semantics remain unchanged except for real defects.
7. POS local support diagnostics expose only operational counts/timestamps and never PIN material, grant IDs, session tokens/hashes, user IDs, permission payloads, or signing material.

## Deferred to later ordered V1 production-hardening domains

Runtime secret delivery/rotation procedures, dependency-CVE review, deployment secret stores, operational key rollover, and broader application audit/observability are owned by the later **Security / Deployment / Observability** domains. This transfer does not reopen the certified Auth/Az authority semantics above.

## Release decision

**Authentication / Authorization V1 is RELEASE CERTIFIED.** All matrix rows are certified, Central remains identity/permission/recovery authority, POS offline authorization is constrained to verified Central grants and local bounded sessions, Frontend is non-authoritative, cross-tenant boundaries are certified, and no unresolved critical Auth/Az defect remains in this domain scope.

Freeze this domain except for real defects. Proceed next to **Frontend screen / error / offline / sync completion V1**.
