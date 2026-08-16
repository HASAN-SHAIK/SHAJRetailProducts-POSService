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
| Branch/store canonical authority | Central branch records and branch-scoped runtime/configuration paths already exist and are used by Inventory, Catalog and effective configuration; Backend #56 protects canonical branch creation with Central admin authority | PARTIAL | add focused Store/Device authority acceptance for create/read/update/lifecycle and tenant isolation |
| First-run POS registration request | Backend #51 certifies token-bound PENDING request behavior, duplicate pending handling and registration lifecycle | CERTIFIED | preserve focused Central acceptance |
| Admin approve/reject | Backend #51 certifies licensing before approval plus PENDING-only rejection | CERTIFIED | preserve focused Central acceptance |
| POS registration claim | Backend #51 certifies token-bound single-use APPROVED -> CLAIMED behavior | CERTIFIED | preserve focused Central acceptance |
| Device licensing / plan limits | Backend #51 certifies existing branch plan/override licensing behavior before approval | CERTIFIED | preserve licensing acceptance while lifecycle evolves |
| Trusted device -> branch binding | Central `resolveDevice(..., requireActive)` is used by certified Inventory/Catalog/Pricing paths; Backend #53 fails closed on ambiguous multiple-active legacy registrations | CERTIFIED | preserve cross-domain and reassignment regressions |
| POS sync machine identity | Backend #52 requires every genuinely new POS sync event to come from an active Central-registered device while retaining exact duplicate/lost-ack detection | CERTIFIED | preserve device/tenant binding and replay invariant |
| Device revocation / deactivation | Backend #52/#54 require active Central registration for new sync/interactive authority; POS #196 certifies a revoked-device-style Central 403 fails closed without replacing the last accepted local configuration, records the failure diagnostically, and preserves the accepted snapshot across SQLite restart | CERTIFIED | add real Central route acceptance while preserving these revocation/replay invariants |
| Device reassignment between branches | Backend #53 prevents one active device ID on two branches; reassignment requires old registration deactivation before new registration; ambiguous legacy registrations fail closed | CERTIFIED | preserve explicit Central-controlled reassignment semantics |
| Terminal identity | Backend registration approval assigns `terminal_id`; POS #200 certifies the approved store/terminal assignment becomes active locally and survives SQLite restart without changing the physical device/installation identity | CERTIFIED | preserve approved identity retention while recovery/replacement semantics are certified |
| Offline retained device/config state | POS #196 certifies last accepted tenant/branch/device effective configuration remains readable after a Central revoked-device 403 and survives SQLite restart; rejected refreshes cannot replace Central authority | CERTIFIED | preserve retained-state/reconnect acceptance while Central remains activation authority |
| Registration/device diagnostics | Backend #58 certifies Central admin support views expose licensing limit/active count, physical device identity, last-seen, active/revoked state, registration lifecycle, branch/terminal assignment, reviewer/review time and claim time; POS #196 preserves configuration-refresh failures in local diagnostics | CERTIFIED | preserve read-only support diagnostics without mutation authority |
| Tenant isolation | Backend #55 certifies that identical physical `device_id` values remain isolated in separate tenant database contexts and cannot collide through registration state | CERTIFIED | preserve tenant-scoped registration plus device-bound Catalog/Inventory/config regressions |
| Interactive tenant browser-device guard | Backend #54 certifies ordinary tenant/browser traffic as validation-only: unregistered or inactive devices cannot be inserted/reactivated and first-run POS approval remains the sole registration authority | CERTIFIED | preserve validation-only browser guard acceptance |
| Frontend admin operations | Frontend #40 certifies the existing admin screen with an exact-head focused test and production build: admin-only access, Central-before-local registration, physical POS identity display, licensing visibility, explicit cross-store reassignment blocking, Central deactivation, loading and actionable errors | CERTIFIED | preserve Frontend as a request/display surface only; Central and POS remain authority owners |
| Recovery/re-registration | Backend #57 requires Central deactivation before a replacement physical POS can receive an already-used logical terminal; the replacement then follows the same Backend #51 request/approval/single-use claim lifecycle, preserving Central licensing and token replay protection | CERTIFIED | preserve Central-controlled replacement and single-use claim semantics |

## Ordered V1 work

1. Add real Central revoked-device configuration/sync reconnect acceptance while preserving the now-certified POS retained offline state.
2. Close Branch/store canonical lifecycle evidence and tenant isolation.
3. Run final Store/Device Operations V1 cross-repository acceptance.
4. Freeze the domain except for real defects.

## Release decision

Store / Device Operations V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, with executable evidence for registration/approval/claim, licensing, revocation, trusted branch binding, offline/reconnect behavior and tenant isolation, and no unresolved critical authority defects.
