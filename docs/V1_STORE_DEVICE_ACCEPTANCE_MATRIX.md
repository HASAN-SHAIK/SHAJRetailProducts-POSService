# SHAJRetailProducts V1 Store / Device Operations Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is authoritative for tenant, branch/store, POS-device registration, activation/revocation, licensing limits, branch assignment/reassignment, recovery, and administrative audit.
- POSService/SQLite is the small offline edge runtime. It may retain the last accepted device identity/configuration needed to continue operating offline, but it may not create or change Central device/branch authority.
- Frontend may request/display administrative actions but is never device-registration, licensing, branch-assignment, or recovery authority.
- Existing browser/interactive tenant-device licensing must not be confused with first-run POS installation approval. V1 acceptance must prove the POS registration path cannot be bypassed by an unrelated auto-registration path.

## Acceptance matrix

| Capability | Existing implementation / evidence | Status | V1 closure requirement |
|---|---|---|---|
| Branch/store canonical authority | Central branch records and branch-scoped runtime/configuration paths already exist and are used by Inventory, Catalog and effective configuration | PARTIAL | add focused Store/Device authority acceptance for create/read/update/lifecycle and tenant isolation |
| First-run POS registration request | Central public `/api/v1/pos-registration/requests` creates a token-bound PENDING request without granting branch authority | NEEDS ACCEPTANCE | certify request token secrecy, duplicate-pending rejection and tenant isolation |
| Admin approve/reject | Central approval requires branch + terminal and calls existing device licensing before setting APPROVED; rejection is PENDING-only | NEEDS ACCEPTANCE | certify admin authority, valid branch assignment, rejection and replay/idempotency |
| POS registration claim | Token-bound claim changes only APPROVED -> CLAIMED | NEEDS ACCEPTANCE | certify wrong token/unapproved/replayed claim fail closed and returned identity matches approved device/branch/terminal |
| Device licensing / plan limits | `ensureDeviceRegistration` enforces branch plan/override limits, supports active/reactivated devices and records device events | NEEDS ACCEPTANCE | certify limits, enterprise/unlimited semantics, inactive reactivation and no count bypass under concurrent/duplicate registration |
| Trusted device -> branch binding | Central `resolveDevice(..., requireActive)` is already used by certified Inventory/Catalog/Pricing paths to derive trusted branch context | CERTIFIED | preserve existing cross-domain evidence; add Store/Device-specific lifecycle regression |
| POS sync machine identity | Certified transaction/inventory/catalog/customer flows use tenant/device machine credentials rather than interactive tenant JWT | CERTIFIED | retain exact device/tenant binding and fail-closed revoked-device acceptance |
| Device revocation / deactivation | `branch_devices.is_active` is consumed by trusted device resolution and licensing code | PARTIAL | identify/administer one canonical revoke path and prove revoked POS cannot sync/config-pull while local offline state remains readable |
| Device reassignment between branches | Central device records are branch-scoped, but lifecycle semantics for moving an existing POS device are not yet certified | GAP | define explicit admin-only reassignment/re-registration behavior; prevent simultaneous active authority in two branches |
| Terminal identity | Registration approval persists `terminal_id` with branch/device request | PARTIAL | establish uniqueness/scope policy and prove claimed POS retains the approved terminal identity |
| Offline retained device/config state | POS already persists effective Central configuration and continues offline using cached accepted state | PARTIAL | certify restart/offline retention and behavior after Central revocation until connectivity resumes; POS must not locally override Central authority |
| Registration/device diagnostics | Central stores registration timestamps/reviewer state and branch device logs; POS has generic sync/config diagnostics | PARTIAL | expose/certify actionable registration, last-seen, revoked/inactive and config-sync support facts without mutation authority |
| Tenant isolation | tenant database context and device registration tables are tenant-scoped; certified device-bound Catalog/Inventory paths already prove branch isolation | PARTIAL | add explicit cross-tenant registration/device-ID collision acceptance |
| Interactive tenant browser-device guard | `branchDeviceGuard` can call `ensureDeviceRegistration(..., mode='register')` on tenant JWT branch requests | GAP | prove this path is intentionally browser-only or change it so an unapproved POS installation cannot obtain POS registration authority through ordinary tenant API traffic |
| Frontend admin operations | existing Frontend/admin device/branch screens require audit before V1 certification | GAP | certify list/pending approval/reject/revoke/reassign/error/loading behavior against Central authority; do not duplicate device state in browser storage |
| Recovery/re-registration | no new recovery mechanism should be invented if existing registration + Central admin lifecycle suffices | GAP | establish lost/replaced-device recovery, token invalidation/replay behavior, and audit trail |

## Ordered V1 work

1. Certify the existing first-run request -> admin approve/reject -> token-bound claim path and licensing limits using focused Central acceptance.
2. Resolve the browser-device-guard versus POS-registration authority boundary so POS approval cannot be bypassed.
3. Certify revoke/deactivate and active-device -> branch binding across sync/config endpoints.
4. Define and certify branch reassignment, terminal identity and replacement/re-registration semantics.
5. Certify offline retained identity/config behavior and reconnect-after-revocation behavior in POS SQLite.
6. Add explicit cross-tenant/device-ID collision acceptance and support diagnostics.
7. Complete Frontend Store/Device administrative UX acceptance using existing Central APIs.
8. Run final Store/Device Operations V1 cross-repository acceptance; freeze the domain except for real defects.

## Release decision

Store / Device Operations V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, with executable evidence for registration/approval/claim, licensing, revocation, trusted branch binding, offline/reconnect behavior and tenant isolation, and no unresolved critical authority defects.
