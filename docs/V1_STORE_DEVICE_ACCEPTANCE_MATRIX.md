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
| First-run POS registration request | Backend #51 certifies token-bound PENDING request behavior, duplicate pending handling and registration lifecycle | CERTIFIED | preserve focused Central acceptance |
| Admin approve/reject | Backend #51 certifies licensing before approval plus PENDING-only rejection | CERTIFIED | preserve focused Central acceptance |
| POS registration claim | Backend #51 certifies token-bound single-use APPROVED -> CLAIMED behavior | CERTIFIED | preserve focused Central acceptance |
| Device licensing / plan limits | Backend #51 certifies existing branch plan/override licensing behavior before approval | CERTIFIED | preserve licensing acceptance while lifecycle evolves |
| Trusted device -> branch binding | Central `resolveDevice(..., requireActive)` is used by certified Inventory/Catalog/Pricing paths; Backend #53 fails closed on ambiguous multiple-active legacy registrations | CERTIFIED | preserve cross-domain and reassignment regressions |
| POS sync machine identity | Backend #52 requires every genuinely new POS sync event to come from an active Central-registered device while retaining exact duplicate/lost-ack detection | CERTIFIED | preserve device/tenant binding and replay invariant |
| Device revocation / deactivation | active registration is required by trusted device resolution, POS sync, catalog feed and effective configuration | PARTIAL | certify revoked POS cannot sync/config-pull while retained local offline state remains readable and reconnect fails closed |
| Device reassignment between branches | Backend #53 prevents one active device ID on two branches; reassignment requires old registration deactivation before new registration; ambiguous legacy registrations fail closed | CERTIFIED | preserve explicit Central-controlled reassignment semantics |
| Terminal identity | Registration approval persists `terminal_id` with branch/device request | PARTIAL | establish uniqueness/scope policy and prove claimed POS retains the approved terminal identity |
| Offline retained device/config state | POS already persists effective Central configuration and continues offline using cached accepted state | PARTIAL | certify restart/offline retention and behavior after Central revocation until connectivity resumes; POS must not locally override Central authority |
| Registration/device diagnostics | Central stores registration timestamps/reviewer state and branch device logs; POS has generic sync/config diagnostics | PARTIAL | expose/certify actionable registration, last-seen, revoked/inactive and config-sync support facts without mutation authority |
| Tenant isolation | tenant database context and device registration tables are tenant-scoped; certified device-bound Catalog/Inventory paths already prove branch isolation | PARTIAL | add explicit cross-tenant registration/device-ID collision acceptance |
| Interactive tenant browser-device guard | Backend #54 changes ordinary tenant/browser traffic to validate pre-existing registration only; exact-head validation pending | PARTIAL | merge only after exact-head Store/Device + Backend CI are green, then mark CERTIFIED |
| Frontend admin operations | existing Frontend/admin device/branch screens require audit before V1 certification | GAP | certify list/pending approval/reject/revoke/reassign/error/loading behavior against Central authority; do not duplicate device state in browser storage |
| Recovery/re-registration | existing registration/deactivation/re-registration primitives should be reused rather than inventing a second recovery mechanism | GAP | establish lost/replaced-device recovery, token invalidation/replay behavior, and audit trail |

## Ordered V1 work

1. Finish Backend #54 browser-device authority validation so ordinary tenant traffic cannot auto-register/reactivate POS devices.
2. Certify revoke/deactivate across sync/config plus retained offline identity/config and reconnect-after-revocation behavior.
3. Certify terminal identity and replacement/re-registration semantics using existing registration primitives.
4. Add explicit cross-tenant/device-ID collision acceptance and support diagnostics.
5. Complete Frontend Store/Device administrative UX acceptance using existing Central APIs.
6. Close Branch/store canonical lifecycle evidence and run final Store/Device Operations V1 cross-repository acceptance.
7. Freeze the domain except for real defects.

## Release decision

Store / Device Operations V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, with executable evidence for registration/approval/claim, licensing, revocation, trusted branch binding, offline/reconnect behavior and tenant isolation, and no unresolved critical authority defects.
