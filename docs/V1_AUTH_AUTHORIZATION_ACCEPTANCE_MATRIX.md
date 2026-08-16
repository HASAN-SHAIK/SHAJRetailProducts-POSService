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
| Role -> permission authority | Backend #61 aligns supported tenant roles with the Central catalog; Backend #66 enforces that catalog on core order/customer/product-read/inventory-read server routes while retaining stricter admin-only catalog/stock writes; POS #207 plus this acceptance gate enforce offline sale/discount/void/refund permissions server-side | PARTIAL | continue audit of remaining V1 Central modules before marking the whole application permission surface certified |
| Branch/store authorization | Backend #66 pins every restricted tenant role (`cashier`, `manager`, `staff`, or any user with `all_branch_access=false`) to its assigned branch regardless of caller-selected branch headers/query/body, while all-branch authority may select intended branches | CERTIFIED | frozen except real defects; cross-tenant isolation remains separately tracked |
| POS offline grant issuance | Backend #62 resolves the requested POS through active Central registration, rejects revoked/unregistered devices and restricted-user branch mismatch, and narrows the signed offline grant to the trusted device branch | CERTIFIED | frozen except real defects |
| Offline grant cryptography | POS #207 certifies RS256 verification plus required issuer, audience, expiry, grant type and claims; expired/wrong issuer/wrong audience/wrong type/forged-key grants fail closed | CERTIFIED | signing-key rotation/operations remain tracked under secrets hardening |
| Offline grant device binding | Backend #62 binds issuance to the active registered device/branch; POS #207 certifies copied grants cannot enroll on a different physical device/store scope | CERTIFIED | frozen except real defects |
| Local PIN enrollment | POS #207 certifies 4-8 digit PIN policy, PBKDF2-SHA256 derived storage with random salt, no plaintext PIN persistence, invalid/expired grant rejection and re-enrollment replacement behavior | CERTIFIED | frozen except real defects |
| Local login/session lifecycle | POS #207 certifies failed-attempt lockout, random hashed session tokens, grant-bounded session lifetime, logout and re-enrollment invalidation | CERTIFIED | disabled-user/device effects on already-issued offline sessions remain tracked under revocation interaction |
| Offline permission enforcement | POS Auth acceptance certifies cashier ordinary sale, dedicated discount permission, cashier void/refund denial, direct privileged void/refund permission, approval-scope mismatch rejection, and required reasons using the existing handlers without changing manager-approval semantics | CERTIFIED | frozen except real defects |
| Online permission enforcement | Backend #65 removes unauthenticated tenant registration; Backend #66 adds server-side permission guards to core order/customer/product-read/inventory-read boundaries and keeps existing stricter admin-only product/import/stock writes | PARTIAL | audit remaining V1 Central protected modules and close any privileged write path still relying only on broad tenant authentication |
| Authentication/device revocation interaction | Store/Device V1 certifies revoked devices lose new sync/config authority while retained offline config remains local; #62 blocks new offline grants to revoked devices | PARTIAL | define/certify disabled user/tenant/device and permission changes versus already-issued offline grants and local sessions |
| Frontend authentication UX | Existing Frontend login/session/route guard flows are present; Frontend #41 is validating that cached Central user data alone cannot bypass POS local session authority while offline and that logout clears the local POS session | PARTIAL | merge #41 only when exact-head Auth/build gates are green; certify permission-based visibility/error UX without treating UI as authority |
| Secrets/cookie/token hardening | Backend #64 removes admin-key fallback and isolates platform-admin signing from tenant JWT signing; tenant/offline keys and HttpOnly cookies already exist | PARTIAL | certify required-secret fail-closed startup/config, SameSite/secure behavior, no token leakage, dependency/key rotation review and production-secret documentation |
| Auth diagnostics/audit | Central/POS already have broader diagnostics/audit infrastructure | GAP | expose actionable read-only auth/session/grant/device failures without credentials/secrets and preserve security-relevant audit evidence |
| Cross-tenant isolation | Tenant DB resolution is derived from verified tenant context and platform admin auth is separate | PARTIAL | executable cross-tenant token/session/grant tests proving no tenant/customer/store/device authority crosses tenant boundaries |

## Closed gaps in this phase

1. **Supported-role mismatch — CLOSED:** Backend #61 now uses the authoritative Central permission catalog consistently in tenant auth and tenant-user provisioning and migrates PostgreSQL role constraints for manager/cashier support.
2. **Offline-grant device authority — CLOSED:** Backend #62 requires an active registered Central POS device, derives the trusted branch from that registration, rejects branch mismatch for restricted users, and narrows all offline grants to one POS branch.
3. **Refresh replay/concurrency — CLOSED:** Backend #63 consumes/rotates refresh tokens under one PostgreSQL row-lock transaction; real concurrent acceptance proves exactly one successor can be issued.
4. **Platform-admin key isolation — CLOSED:** Backend #64 requires a dedicated admin signing key and proves tenant/admin tokens cannot cross verification boundaries.
5. **POS offline authentication baseline — CLOSED:** POS #207 certifies grant cryptography, device/store binding, PIN security, lockout/logout/session invalidation and ordinary-sale/discount permission boundaries.
6. **Public tenant-user registration bypass — CLOSED:** Backend #65 removes both public `/register` routes; V1 tenant-user creation remains under authenticated tenant-admin management.
7. **Core protected-route permission bypass — CLOSED:** Backend #66 enforces Central role permissions on the V1 order/customer/product-read/inventory-read surfaces instead of relying on Frontend visibility.
8. **Restricted-role branch selection — CLOSED:** Backend #66 pins all restricted tenant roles to the branch carried by Central identity authority.
9. **POS sensitive permission evidence — CLOSED:** the V1 Auth gate explicitly runs existing void/refund permission and approval-scope tests without expanding manager-approval behavior.

## Next proven gap

**Revocation versus already-issued offline authority:** new grants/config/sync already fail for revoked devices, but V1 still needs an explicit policy and executable evidence for disabled users/tenants/devices or permission changes after an offline grant/local session has already been issued.

## Ordered V1 work

1. Close disabled user/tenant/device + already-issued offline grant/session semantics.
2. Finish Frontend authentication/offline-login UX certification and actionable 401/403 handling through #41.
3. Complete the remaining Central protected-module permission audit.
4. Complete secret/key/cookie/dependency review, auth diagnostics/audit and cross-tenant isolation.
5. Run final Authentication / Authorization V1 cross-repository release acceptance, then freeze except for real defects.

## Release decision

Authentication / Authorization V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, all privileged decisions are enforced outside the Frontend, Central remains identity/permission/recovery authority, POS offline authorization is limited to valid Central-signed grants bound to the installed device/store scope, and no unresolved critical authentication or authorization defect remains.
