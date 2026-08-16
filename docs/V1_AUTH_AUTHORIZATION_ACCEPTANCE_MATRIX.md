# SHAJRetailProducts V1 Authentication / Authorization Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is authoritative for tenant identity, interactive authentication, refresh/session lifecycle, platform-admin identity, role/permission grants, branch/store scope, POS-device eligibility, offline-grant issuance/revocation policy, and recovery authority.
- POSService/SQLite may verify a Central-signed offline grant, bind it to the installed POS identity and configured tenant, enroll a local PIN credential, create short-lived local sessions, and enforce the granted permission/store scope while offline. POS must not invent tenant users, permissions, branch scope, tenant identity, or Central device authority.
- Frontend may collect credentials and render/guard routes/actions from authoritative session/permission state, but must not be the security boundary for protected Central or POS mutations.

## Acceptance matrix

| Capability | Existing implementation / evidence | Status | V1 closure requirement |
|---|---|---|---|
| Tenant login + access JWT | Backend #61 aligns tenant authentication, admin provisioning, persisted PostgreSQL role constraints and fresh-tenant provisioning with the authoritative `admin`/`manager`/`cashier`/transitional-`staff` permission catalog; Backend #71 additionally requires current active Central tenant state | CERTIFIED | frozen except real defects |
| Refresh-token lifecycle | Backend #63 atomically locks and consumes the predecessor refresh token, revokes it and inserts exactly one successor in one PostgreSQL transaction; real PostgreSQL acceptance proves concurrent use yields one successor and replay/expired predecessors cannot rotate | CERTIFIED | frozen except real defects |
| Platform-admin authentication | Backend #64 requires a dedicated `ADMIN_JWT_SECRET`, rejects tenant/admin token crossover, recognizes only Central platform-admin roles, removes credential-bearing admin logging, and passed exact-head V1 Auth + control-plane CI | CERTIFIED | startup/rotation documentation remains tracked under secrets hardening |
| Role -> permission authority | Backend #61 aligns supported tenant roles with the Central catalog; Backend #66 protects core order/customer/product/inventory routes; #68 protects purchase/expense/report; #69 accounting; #72 staff/salary/settings/GST/corrections; #73 legacy returns; POS #207/#209 enforce offline sale/discount/void/refund permissions server-side | CERTIFIED | frozen except real defects; no new permission taxonomy introduced |
| Branch/store authorization | Backend #66 pins restricted tenant roles to their assigned branch regardless of caller-selected branch metadata, while all-branch authority may select intended branches | CERTIFIED | frozen except real defects |
| POS offline grant issuance | Backend #62 requires an active registered Central POS and trusted branch; Backend #67 reloads current Central user role/branch immediately before signing so stale JWT authority cannot mint a fresh elevated grant | CERTIFIED | frozen except real defects |
| Offline grant cryptography | POS #207 certifies RS256 verification plus required issuer, audience, expiry, grant type and claims; expired/wrong issuer/wrong audience/wrong type/forged-key grants fail closed | CERTIFIED | signing-key operations remain tracked under secrets hardening |
| Offline grant device/tenant binding | Backend #62 binds issuance to active registered device/branch; POS #207 certifies device/store binding; POS #212 additionally requires the grant tenant to equal the packaged POS runtime's configured Central tenant before persisting a local user | CERTIFIED | frozen except real defects |
| Local PIN enrollment | POS #207 certifies 4-8 digit PIN policy, PBKDF2-SHA256 derived storage with random salt, no plaintext PIN persistence, invalid/expired grant rejection and re-enrollment replacement behavior | CERTIFIED | frozen except real defects |
| Local login/session lifecycle | POS #207 certifies failed-attempt lockout, random hashed session tokens, grant-bounded session lifetime, logout and re-enrollment invalidation | CERTIFIED | frozen except real defects |
| Offline permission enforcement | POS #209 certifies cashier ordinary sale, dedicated discount permission, cashier void/refund denial, direct privileged void/refund permission, approval-scope mismatch rejection and required reasons using the existing handlers without changing manager-approval semantics | CERTIFIED | frozen except real defects |
| Online permission enforcement | Backend #65 removes unauthenticated tenant registration; #66 protects core transaction/customer/catalog/inventory boundaries; #68 procurement/expense/reporting; #69 accounting; #72 staff/salary/settings/GST/corrections; #73 legacy return mutation/history | CERTIFIED | frozen except real defects; Frontend visibility is not authority |
| Authentication/device revocation interaction | Central immediately blocks new grants/config/sync after revoked device/current-authority changes; POS disconnected authority is explicitly bounded to signed grant expiry and each local session to the lesser of 12 hours or grant expiry; re-enrollment invalidates prior sessions | CERTIFIED | instantaneous disconnected revocation is intentionally not claimed |
| Frontend authentication UX | Frontend #41 requires a POSService-validated local session for protected offline routes, keeps explicit PIN login as session creation authority, clears local POS session on logout, and retains centralized 401/403 handling | CERTIFIED | frozen except real defects; UI remains non-authoritative |
| Secrets/cookie/token hardening | Backend #64 removes admin-key fallback and isolates platform-admin signing; tenant and refresh cookies are HttpOnly with production Secure and SameSite=Lax; POS packaged runtime uses loopback-only local API plus generated/persisted machine token | PARTIAL | certify tenant JWT/offline key fail-closed startup, browser token exposure policy, key rotation/production-secret guidance and dependency review |
| Auth diagnostics/audit | Central/POS already have broader diagnostics/audit infrastructure and POS sync diagnostics, but no focused credential-safe Auth/Az support contract is certified | GAP | expose/certify actionable read-only auth/session/grant/device failures without secrets/tokens and preserve security-relevant audit evidence |
| Cross-tenant isolation | Backend #74 certifies tenant access context comes only from the verified tenant token, refresh tenant selection comes from the refresh-token tenant prefix, and platform-admin verification stays on its dedicated key; POS #212 rejects a valid Central offline grant from a different configured tenant before local persistence | CERTIFIED | frozen except real defects |

## Closed gaps in this phase

1. **Supported-role mismatch — CLOSED:** Backend #61 aligns tenant authentication and provisioning to the Central permission catalog.
2. **Offline-grant device authority — CLOSED:** Backend #62 requires an active registered Central POS and trusted branch.
3. **Refresh replay/concurrency — CLOSED:** Backend #63 consumes/rotates refresh tokens under one PostgreSQL row-lock transaction.
4. **Platform-admin key isolation — CLOSED:** Backend #64 requires a dedicated admin signing key and rejects tenant/admin token crossover.
5. **POS offline authentication baseline — CLOSED:** POS #207 certifies grant cryptography, device/store binding, PIN security, lockout/logout/session invalidation and ordinary-sale/discount permission boundaries.
6. **Public tenant-user registration bypass — CLOSED:** Backend #65 removes both public tenant registration routes.
7. **Core protected-route permission bypass — CLOSED:** Backend #66 enforces Central role permissions on V1 core protected surfaces.
8. **Restricted-role branch selection — CLOSED:** Backend #66 pins restricted tenant roles to Central-authorized branch scope.
9. **POS sensitive permission evidence — CLOSED:** POS #209 certifies void/refund permission and existing approval-scope behavior without expanding manager approval.
10. **Stale-authority offline grant amplification — CLOSED:** Backend #67 reloads current Central user role/branch before signing.
11. **Frontend offline cached-user bypass — CLOSED:** Frontend #41 validates the POS local session before protected offline access.
12. **Purchase/expense/report permission bypass — CLOSED:** Backend #68 applies existing catalog authorities.
13. **Accounting permission boundary — CLOSED:** Backend #69 keeps accounting mutations Central-admin while read-only books/statements use existing reporting authority.
14. **Tenant-auth device authority bypass — CLOSED:** Backend #70 makes tenant-auth device checks validation-only.
15. **Stale tenant-state authorization — CLOSED:** Backend #71 resolves current Central tenant state and rejects disabled tenants.
16. **Remaining admin/finance route surfaces — CLOSED:** Backend #72 protects staff, salary, settings, GST and correction surfaces with existing authorities/admin scope.
17. **Legacy Central return mutation bypass — CLOSED:** Backend #73 requires existing `pos:refund` for mutation and `orders:read` for history without changing POS approval semantics.
18. **Already-issued offline revocation ambiguity — CLOSED:** disconnected authority is bounded to signed grant/session expiry rather than claiming impossible instantaneous offline revocation.
19. **Cross-tenant access/grant isolation — CLOSED:** Backend #74 binds online/refresh tenant context to signed token identity and POS #212 binds offline enrollment to the configured Central tenant.

## Next proven gap

**Secrets/token hardening and Auth diagnostics:** the broad application permission surface and cross-tenant identity boundaries are now certified. Remaining Auth/Az release work is to close browser/access-token exposure and required-key/startup/rotation policy, then certify credential-safe operational Auth diagnostics/audit.

## Ordered V1 work

1. Complete secret/key/cookie/browser-token/dependency hardening and production-secret/rotation guidance.
2. Certify credential-safe Auth/Az diagnostics and security audit evidence across Central/POS.
3. Run final Authentication / Authorization V1 cross-repository release acceptance, then freeze except for real defects.

## Release decision

Authentication / Authorization V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, all privileged decisions are enforced outside the Frontend, Central remains identity/permission/recovery authority, POS offline authorization is limited to valid Central-signed grants bound to installed device/store/tenant scope, and no unresolved critical authentication or authorization defect remains.
