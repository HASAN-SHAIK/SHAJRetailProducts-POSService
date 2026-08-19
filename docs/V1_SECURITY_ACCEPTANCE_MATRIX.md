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
| Secret defaults/fail-closed startup | `JWT_SECRET` already fails closed; remaining Central/POS secret-bearing configuration requires repository-wide audit | PARTIAL | no production fallback/default secrets; missing required secrets must fail startup/feature boundary |
| Cookie/CORS/CSRF boundary | Browser Auth uses HttpOnly `Secure`/`SameSite=Lax`; CORS/credential policy requires consolidated V1 acceptance | PARTIAL | certify allowed origins/credential behavior and reject unsafe cross-origin mutation paths |
| Central login/brute-force protection | `express-rate-limit` dependency exists; exact login/refresh/reset route coverage is not yet V1-certified | GAP | bounded auth-sensitive request rates without blocking health/sync operation |
| Dependency/supply-chain review | Go modules and npm manifests/lockfiles exist across repos, but V1 vulnerability/dependency policy is not yet certified | GAP | audit runtime dependencies, remove clearly unused/risky packages where safe, document accepted residuals |
| Upload/import input boundary | Product import and existing upload handlers have size/type validation in places; server-side filename/content/size authority needs consolidated audit | PARTIAL | bound upload sizes/types and avoid path/content execution trust |
| SQL/query injection boundary | Primary services use parameterized PostgreSQL queries in certified paths; repository-wide dynamic SQL/support/reporting audit remains open | PARTIAL | no caller-controlled SQL identifiers/fragments without strict allowlists |
| Security-sensitive logging | Auth/Observability already removed password-hash/access-token/query leakage in certified paths | CERTIFIED | preserve credential-safe structured logging |
| Recovery/approval authority separation | Inventory/Store/Device/Auth/Database domains keep Central-approved recovery separate from read-only support views | CERTIFIED | do not re-expand support or manager-approval authority |
| Frontend secret persistence | Frontend Completion/Auth V1 certifies no Central JWT in JS-readable browser persistence | CERTIFIED | preserve metadata-only browser session persistence |
| Security CI/release gate | no final consolidated Security release gate yet | GAP | merged Backend + POS + Frontend security acceptance/build must be green |

## Initial audit priorities

1. Audit Central secret defaults, cookie/CORS policy and auth-sensitive rate limiting.
2. Audit runtime dependencies and clearly suspicious/unused packages before changing versions blindly.
3. Audit upload/import and dynamic-query boundaries that accept caller-controlled text/files.
4. Add focused cross-tenant/store/key/token acceptance only where existing domain evidence does not already cover the boundary.
5. Add final merged-main Security release gate and mark every row CERTIFIED or explicitly justified N/A/accepted residual.

## Release decision

**NOT YET RELEASE CERTIFIED.** Core tenant/store/device/token/session/recovery security is already strongly covered by earlier certified domains. Remaining V1 Security work is concentrated on secret/config defaults, CORS/cookie/rate-limit hardening, dependency/supply-chain review, upload/query boundary audit and the final consolidated release gate.
