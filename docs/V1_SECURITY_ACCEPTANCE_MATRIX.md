# SHAJRetailProducts V1 Security Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central owns tenant identity, roles/permissions, POS registration, branch/store trust, offline-grant signing, canonical recovery authorization and tenant PostgreSQL authority.
- POSService is a small offline edge runtime. It verifies Central-issued grants, persists only local derived/session state required for offline operation, and must never become a tenant/permission/key authority.
- Frontend is presentation/orchestration only. Central browser credentials remain HttpOnly-cookie based and local POS access remains bound to the packaged POS local session/machine boundary.
- Security work must reuse already-certified Auth/Az, Store/Device, Inventory, Catalog, Database and Observability boundaries rather than introduce parallel security models.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| Tenant database isolation | Customers/Reporting/Auth certifications prove supplied tenant pools remain isolated and cross-tenant token/grant use fails closed | CERTIFIED | retain cross-tenant PostgreSQL/session/grant acceptance |
| Store/branch isolation | Store/Device, Inventory, Catalog and Reporting certifications bind restricted users/devices to trusted Central branches | CERTIFIED | preserve trusted branch authority on reads/writes/sync |
| POS device identity and revocation | Store/Device V1 certifies first-run registration, active-device sync authority, explicit reassignment, revocation and replacement | CERTIFIED | preserve active Central registration requirement for new machine facts |
| Central tenant browser session | Auth/Az V1 certifies HttpOnly cookie-only access tokens, refresh rotation/replay protection and 401/403 handling | CERTIFIED | preserve secure cookie flags and token replay protection |
| Platform-admin key isolation | Auth/Az V1 certifies platform-admin signing/verification does not fall back to tenant JWT secret | CERTIFIED | preserve separate key boundary |
| Offline grant signing and scope | Auth/Az V1 certifies Central current-user/device/branch authority, RS256 verification, tenant/device/store binding and expiry | CERTIFIED | preserve current-authority reload before grant issuance |
| POS local PIN/session security | Auth/Az V1 certifies PBKDF2-derived PIN storage, lockout, hashed local sessions, logout and re-enrollment invalidation | CERTIFIED | preserve no plaintext PIN/session-token persistence |
| POS local machine/origin boundary | Auth/Az V1 certifies protected loopback routes require machine token and approved browser origin | CERTIFIED | preserve public health/readiness exceptions only |
| Request/error/diagnostic secret hygiene | Observability V1 certifies stable 5xx responses, safe bounded request IDs and credential-safe diagnostics | CERTIFIED | preserve no secret/query/payload leakage in support surfaces |
| Migration/backup tenant protection | Database Quality V1 certifies checksum drift rejection, same-tenant native restore and cross-tenant restore protection | CERTIFIED | preserve verified manifest/checksum/tenant binding |
| Secret defaults/fail-closed startup | Auth/Az requires Central tenant/platform JWT secrets; POS #289 requires the offline-grant public key in production and rejects placeholder sync/local secrets; Backend #108 requires an explicit production RabbitMQ URL when messaging is enabled and rejects embedded `guest:guest` credentials | PARTIAL | finish repository-wide production secret/config audit; no production fallback/default secrets |
| Cookie/CORS/CSRF boundary | Auth/Az certifies HttpOnly `Secure`/`SameSite=Lax` browser cookies; Backend #106 rejects production credentialed wildcard CORS and permits only explicitly configured browser origins while retaining no-Origin machine clients | CERTIFIED | preserve explicit production origin allowlist and secure cookie flags |
| Central login/brute-force protection | Backend #105 shares bounded login/refresh limiters across legacy and `/api/v1` tenant auth aliases; failed logins are capped without consuming allowance on successful logins; platform-admin login was already bounded | CERTIFIED | retain auth-sensitive rate limits without blocking health/sync operation |
| Dependency/supply-chain review | POS Go dependencies are checksum-pinned; Frontend #78 builds its production container on Node 24 with committed lockfile + `npm ci`; Backend #110 now commits `package-lock.json`, validates it with `npm ci`/`npm ls`, and builds production with `npm ci --omit=dev`; Frontend still declares `jsonwebtoken` using the floating `latest` spec and advisory/runtime-reachability review remains open | PARTIAL | eliminate remaining floating production specs, audit current advisories/runtime reachability and document accepted residuals |
| Upload/import input boundary | Central JSON bodies are capped at 5 MB; Backend #107 caps the admin offline product import at 500 rows before DB work; Backend #109 keeps purchase-invoice uploads memory-only/5 MB and requires consistent supported MIME + filename extension before PDF/OCR processing | PARTIAL | finish any remaining file/content/path handler audit; do not trust filename/content for execution |
| SQL/query injection boundary | Primary services use parameterized PostgreSQL queries in certified paths; the data-quality backup's dynamic table SQL interpolates only an internal fixed table allowlist; repository-wide dynamic SQL audit remains open | PARTIAL | no caller-controlled SQL identifiers/fragments without strict allowlists |
| Security-sensitive logging | Auth/Observability already removed password-hash/access-token/query leakage in certified paths | CERTIFIED | preserve credential-safe structured logging |
| Recovery/approval authority separation | Inventory/Store/Device/Auth/Database domains keep Central-approved recovery separate from read-only support views | CERTIFIED | do not re-expand support or manager-approval authority |
| Frontend secret persistence | Frontend Completion/Auth V1 certifies no Central JWT in JS-readable browser persistence | CERTIFIED | preserve metadata-only browser session persistence |
| Security CI/release gate | focused Backend, POS and Frontend Security workflows now exist; no final consolidated three-repository Security release gate yet | PARTIAL | merged Backend + POS + Frontend security acceptance/build must be green |

## Current audit priorities

1. Eliminate the remaining Frontend floating dependency spec and complete dependency-advisory/runtime-reachability review.
2. Finish remaining file/content/path and dynamic-query boundaries accepting caller-controlled input.
3. Finish the production secret/config review across the three repositories.
4. Add final merged-main Security release gate and mark every row CERTIFIED or explicitly justified N/A/accepted residual.

## Release decision

**NOT YET RELEASE CERTIFIED.** Core tenant/store/device/token/session/recovery security is strongly covered; auth brute-force, browser cookie/CORS, bounded import/upload admission, production RabbitMQ credential safety, Frontend lockfile-enforced container builds and Backend lockfile-enforced CI/container builds now have executable evidence. Remaining V1 Security work is concentrated on the Frontend floating/advisory dependency review, remaining input/query/config audit and the final consolidated release gate.
