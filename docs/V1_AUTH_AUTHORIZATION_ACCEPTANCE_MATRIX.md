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
| Refresh-token lifecycle | Backend #63 atomically locks and consumes the predecessor refresh token, revokes it and inserts exactly one successor in one PostgreSQL transaction; real PostgreSQL acceptance proves concurrent use yields one successor and replay/expired predecessors cannot rotate | CERTIFIED | logout/revocation and tenant-isolation evidence remains tracked under revocation/isolation rows |
| Platform-admin authentication | Backend #64 requires a dedicated `ADMIN_JWT_SECRET`, rejects tenant/admin token crossover, recognizes only Central platform-admin roles, removes credential-bearing admin logging, and passed exact-head V1 Auth + control-plane CI | CERTIFIED | frozen except real defects; startup/rotation documentation remains tracked under secrets hardening |
| Role -> permission authority | Backend #61 aligns supported tenant roles with the Central catalog; Backend #66 enforces that catalog on core order/customer/product-read/inventory-read server routes; Backend #68 extends explicit authority to purchase/expense/report surfaces; POS #207/#209 enforce offline sale/discount/void/refund permissions server-side | PARTIAL | accounting authority is under focused validation in Backend #69; continue audit of any remaining protected V1 Central modules before marking the whole application permission surface certified |
| Branch/store authorization | Backend #66 pins every restricted tenant role (`cashier`, `manager`, `staff`, or any user with `all_branch_access=false`) to its assigned branch regardless of caller-selected branch headers/query/body, while all-branch authority may select intended branches | CERTIFIED | frozen except real defects; cross-tenant isolation remains separately tracked |
| POS offline grant issuance | Backend #62 resolves the requested POS through active Central registration and trusted branch; Backend #67 reloads the current Central user role/branch immediately before signing so stale JWT role or branch claims cannot mint fresh elevated offline grants | CERTIFIED | frozen except real defects |
| Offline grant cryptography | POS #207 certifies RS256 verification plus required issuer, audience, expiry, grant type and claims; expired/wrong issuer/wrong audience/wrong type/forged-key grants fail closed | CERTIFIED | signing-key rotation/operations remain tracked under secrets hardening |
| Offline grant device binding | Backend #62 binds issuance to the active registered device/branch; POS #207 certifies copied grants cannot enroll on a different physical device/store scope | CERTIFIED | frozen except real defects |
| Local PIN enrollment | POS #207 certifies 4-8 digit PIN policy, PBKDF2-SHA256 derived storage with random salt, no plaintext PIN persistence, invalid/expired grant rejection and re-enrollment replacement behavior | CERTIFIED | frozen except real defects |
| Local login/session lifecycle | POS #207 certifies failed-attempt lockout, random hashed session tokens, grant-bounded session lifetime, logout and re-enrollment invalidation | CERTIFIED | frozen except real defects |
| Offline permission enforcement | POS #209 certifies cashier ordinary sale, dedicated discount permission, cashier void/refund denial, direct privileged void/refund permission, approval-scope mismatch rejection, and required reasons using the existing handlers without changing manager-approval semantics | CERTIFIED | frozen except real defects |
| Online permission enforcement | Backend #65 removes unauthenticated tenant registration; Backend #66 adds server-side permission guards to core order/customer/product-read/inventory-read boundaries; Backend #68 adds explicit purchase/expense/report guards | PARTIAL | Backend #69 is validating the accounting read/mutation boundary; finish remaining protected-module audit |
| Authentication/device revocation interaction | Central immediately blocks new grants/config/sync after revoked device/current-authority changes; POS local authority cannot receive live revocation while disconnected, so V1 explicitly bounds already-issued authority to the signed grant expiry and bounds each local session to the lesser of 12 hours or that grant expiry; re-enrollment invalidates existing sessions | CERTIFIED | frozen except real defects; instantaneous offline revocation is intentionally not claimed because it would require online authority |
| Frontend authentication UX | Frontend #41 requires a POSService-validated local session for protected offline routes, keeps explicit PIN login as session creation authority, clears local POS session on logout, and retains centralized 401/403 handling; dedicated Auth/build and regressions passed | CERTIFIED | frozen except real defects; UI remains non-authoritative |
| Secrets/cookie/token hardening | Backend #64 removes admin-key fallback and isolates platform-admin signing from tenant JWT signing; tenant/offline keys and HttpOnly cookies already exist | PARTIAL | certify required-secret fail-closed startup/config, SameSite/secure behavior, no token leakage, dependency/key rotation review and production-secret documentation |
| Auth diagnostics/audit | Central/POS already have broader diagnostics/audit infrastructure | GAP | expose actionable read-only auth/session/grant/device failures without credentials/secrets and preserve security-relevant audit evidence |
| Cross-tenant isolation | Tenant DB resolution is derived from verified tenant context and platform admin auth is separate | PARTIAL | executable cross-tenant token/session/grant tests proving no tenant/customer/store/device authority crosses tenant boundaries |

## Closed gaps in this phase

1. **Supported-role mismatch — CLOSED:** Backend #61 now uses the authoritative Central permission catalog consistently in tenant auth and tenant-user provisioning and migrates PostgreSQL role constraints for manager/cashier support.
2. **Offline-grant device authority — CLOSED:** Backend #62 requires an active registered Central POS device and derives the trusted branch from that registration.
3. **Refresh replay/concurrency — CLOSED:** Backend #63 consumes/rotates refresh tokens under one PostgreSQL row-lock transaction; real concurrent acceptance proves exactly one successor can be issued.
4. **Platform-admin key isolation — CLOSED:** Backend #64 requires a dedicated admin signing key and proves tenant/admin tokens cannot cross verification boundaries.
5. **POS offline authentication baseline — CLOSED:** POS #207 certifies grant cryptography, device/store binding, PIN security, lockout/logout/session invalidation and ordinary-sale/discount permission boundaries.
6. **Public tenant-user registration bypass — CLOSED:** Backend #65 removes both public `/register` routes; V1 tenant-user creation remains under authenticated tenant-admin management.
7. **Core protected-route permission bypass — CLOSED:** Backend #66 enforces Central role permissions on the V1 order/customer/product-read/inventory-read surfaces instead of relying on Frontend visibility.
8. **Restricted-role branch selection — CLOSED:** Backend #66 pins all restricted tenant roles to the branch carried by Central identity authority.
9. **POS sensitive permission evidence — CLOSED:** POS #209 explicitly certifies void/refund permission and approval-scope behavior without expanding manager-approval semantics.
10. **Stale-authority offline grant amplification — CLOSED:** Backend #67 reloads the current Central user from PostgreSQL immediately before signing and derives role/permissions/branch from current authority, preventing stale manager/old-branch JWTs from minting fresh elevated grants.
11. **Frontend offline cached-user bypass — CLOSED:** Frontend #41 validates the local POS session with POSService before allowing protected offline routes and clears that session on logout.
12. **Purchase/expense/report permission bypass — CLOSED:** Backend #68 reuses existing permission catalog authorities for procurement, expenses and reports instead of relying on Frontend visibility.
13. **Already-issued offline revocation ambiguity — CLOSED:** V1 explicitly accepts bounded disconnected authority: new Central authority is revoked immediately online, while an already-issued offline grant remains usable only until its signed expiry and each local session is capped at 12 hours or the grant expiry; focused SQLite acceptance proves session and future PIN login fail closed at the stored grant-expiry boundary.

## Next proven gap

**Remaining Central protected-module authorization:** Backend #68 is merged and Backend #69 is validating the accounting boundary. After that, the remaining Auth/Az release risks are secrets/cookie/key hardening, actionable auth diagnostics/audit, and executable cross-tenant token/session/grant isolation.

## Ordered V1 work

1. Finish Backend #69 accounting authorization and audit any remaining Central protected V1 modules.
2. Complete secret/key/cookie/dependency review, auth diagnostics/audit and cross-tenant isolation.
3. Run final Authentication / Authorization V1 cross-repository release acceptance, then freeze except for real defects.

## Release decision

Authentication / Authorization V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, all privileged decisions are enforced outside the Frontend, Central remains identity/permission/recovery authority, POS offline authorization is limited to valid Central-signed grants bound to the installed device/store scope, and no unresolved critical authentication or authorization defect remains.
