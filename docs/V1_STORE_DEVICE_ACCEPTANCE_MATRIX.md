# SHAJRetailProducts V1 Store / Device Operations Acceptance Matrix

Status: **RELEASE CERTIFIED — FROZEN EXCEPT FOR REAL DEFECTS**

## Authority boundary

- Central Backend/PostgreSQL is authoritative for tenant, branch/store, POS-device registration, activation/revocation, licensing limits, branch assignment/reassignment, recovery, and administrative audit.
- POSService/SQLite is the small offline edge runtime. It may retain the last accepted device identity/configuration needed to continue operating offline, but it may not create or change Central device/branch authority.
- Frontend may request/display administrative actions but is never device-registration, licensing, branch-assignment, or recovery authority.
- Existing browser/interactive tenant-device licensing must not be confused with first-run POS installation approval. V1 acceptance proves the POS registration path cannot be bypassed by an unrelated auto-registration path.

## Acceptance matrix

| Capability | Existing implementation / evidence | Status | V1 closure requirement |
|---|---|---|---|
| Branch/store canonical authority | Backend #56 protects canonical branch creation with Central admin authority; Backend #59 adds `is_active`, admin-only update/soft-deactivation, blocks deactivation while active POS devices remain, rejects POS authority on inactive branches, and makes trusted config/device resolution fail closed for inactive branches while preserving existing device route/response contracts | CERTIFIED | preserve branch lifecycle, tenant isolation, and cross-domain branch-scoped regressions |
| First-run POS registration request | Backend #51 certifies token-bound PENDING request behavior, duplicate pending handling and registration lifecycle | CERTIFIED | preserve focused Central acceptance |
| Admin approve/reject | Backend #51 certifies licensing before approval plus PENDING-only rejection | CERTIFIED | preserve focused Central acceptance |
| POS registration claim | Backend #51 certifies token-bound single-use APPROVED -> CLAIMED behavior | CERTIFIED | preserve focused Central acceptance |
| Device licensing / plan limits | Backend #51 certifies existing branch plan/override licensing behavior before approval | CERTIFIED | preserve licensing acceptance while lifecycle evolves |
| Trusted device -> branch binding | Central `resolveDevice(..., requireActive)` is used by certified Inventory/Catalog/Pricing paths; Backend #53 fails closed on ambiguous multiple-active legacy registrations | CERTIFIED | preserve cross-domain and reassignment regressions |
| POS sync machine identity | Backend #52 requires every genuinely new POS sync event to come from an active Central-registered device while retaining exact duplicate/lost-ack detection | CERTIFIED | preserve device/tenant binding and replay invariant |
| Device revocation / deactivation | Backend #52/#54 require active Central registration for new sync/interactive authority; POS #196 certifies a revoked-device-style Central 403 fails closed without replacing the last accepted local configuration, records the failure diagnostically, and preserves the accepted snapshot across SQLite restart; Backend #60 certifies the production Central POS configuration route rejects revoked/inactive devices with `POS_DEVICE_NOT_REGISTERED` 403 and rejects invalid machine credentials before configuration resolution | CERTIFIED | preserve Central revocation authority, retained offline state, and duplicate/lost-ack replay invariants |
| Device reassignment between branches | Backend #53 prevents one active device ID on two branches; reassignment requires old registration deactivation before new registration; ambiguous legacy registrations fail closed | CERTIFIED | preserve explicit Central-controlled reassignment semantics |
| Terminal identity | Backend registration approval assigns `terminal_id`; POS #200 certifies the approved store/terminal assignment becomes active locally and survives SQLite restart without changing the physical device/installation identity | CERTIFIED | preserve approved identity retention and Central-controlled replacement semantics |
| Offline retained device/config state | POS #196 certifies last accepted tenant/branch/device effective configuration remains readable after a Central revoked-device 403 and survives SQLite restart; rejected refreshes cannot replace Central authority | CERTIFIED | preserve retained-state/reconnect behavior while Central remains activation authority |
| Registration/device diagnostics | Backend #58 certifies Central admin support views expose licensing limit/active count, physical device identity, last-seen, active/revoked state, registration lifecycle, branch/terminal assignment, reviewer/review time and claim time; POS #196 preserves configuration-refresh failures in local diagnostics | CERTIFIED | preserve read-only support diagnostics without mutation authority |
| Tenant isolation | Backend #55 certifies that identical physical `device_id` values remain isolated in separate tenant database contexts and cannot collide through registration state | CERTIFIED | preserve tenant-scoped registration plus device-bound Catalog/Inventory/config regressions |
| Interactive tenant browser-device guard | Backend #54 certifies ordinary tenant/browser traffic as validation-only: unregistered or inactive devices cannot be inserted/reactivated and first-run POS approval remains the sole registration authority | CERTIFIED | preserve validation-only browser guard acceptance |
| Frontend admin operations | Frontend #40 certifies the existing admin screen with an exact-head focused test and production build: admin-only access, Central-before-local registration, physical POS identity display, licensing visibility, explicit cross-store reassignment blocking, Central deactivation, loading and actionable errors | CERTIFIED | preserve Frontend as a request/display surface only; Central and POS remain authority owners |
| Recovery/re-registration | Backend #57 requires Central deactivation before a replacement physical POS can receive an already-used logical terminal; the replacement then follows the same Backend #51 request/approval/single-use claim lifecycle, preserving Central licensing and token replay protection | CERTIFIED | preserve Central-controlled replacement and single-use claim semantics |

## Final V1 release acceptance

The Store / Device Operations release gate executes against all three current V1 repositories:

1. POSService: full Go package/integration regression on the release-candidate head.
2. Central Backend: the complete Store/Device authority acceptance suite from merged Backend `main`, including registration, licensing, branch lifecycle, browser guard, sync active-device authority, revoked configuration reconnect, diagnostics, and recovery/replacement rules.
3. Frontend: the focused Store/Device admin-authority acceptance plus production build from merged Frontend `main`.

## Release decision

**STORE / DEVICE OPERATIONS V1 RELEASE CERTIFIED.** Every matrix row is CERTIFIED with executable evidence. Central remains the only branch/device/licensing/recovery authority; POS remains the retained offline edge identity/runtime; Frontend remains request/display only. Freeze this domain after the final cross-repository release gate is green. Reopen only for a demonstrated defect or an explicitly governed post-V1 capability.
