# SHAJRetailProducts V1 Central ↔ POS Synchronization Acceptance Matrix

Status: **RELEASE CERTIFIED**

## Authority boundary

- Central Backend/PostgreSQL is canonical for tenant, branch, device registration, effective configuration, products/categories/prices/tax facts, canonical customer financial/lifecycle snapshots, and recovery authority.
- POSService/SQLite is the durable offline edge projection. It may retain the last accepted Central state while disconnected, but it must not widen tenant/branch/device scope or invent canonical configuration/catalog truth.
- Frontend is presentation/orchestration only and must not become a second configuration/catalog synchronization engine.
- Outbound POS business facts continue through the already-certified durable outbox. This matrix certifies Central → POS configuration/catalog/customer projection and reconnect consistency.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| Packaged runtime wiring | `cmd/posservice` starts the change-feed puller and effective-config refresh service when Central is configured | CERTIFIED | preserve the packaged runtime as the production sync composition root |
| Effective configuration machine authentication | config client sends tenant/device/sync-token headers and requires HTTPS outside loopback; Store/Device V1 certifies active Central registration on the server side | CERTIFIED | retain active-device Central authority and secure transport |
| Effective configuration scope binding | merged POS #265 rejects returned tenant or device scope that does not match the configured Central tenant/device before persistence | CERTIFIED | tenant/device mismatch must fail closed before replacing accepted state |
| Effective configuration precedence | Pricing/Tax and Store/Device evidence certifies Central tenant → branch → device effective-policy resolution | CERTIFIED | preserve Central precedence and source provenance |
| ETag / 304 refresh | client sends `If-None-Match`; service treats 304 as unchanged success and persists sync state | CERTIFIED | no-op refresh must not replace the accepted snapshot |
| Durable configuration snapshot / restart | SQLite `effective_config_snapshot` stores schema, ETag, tenant/branch/device scope and payload; restart/offline-retention acceptance already exists | CERTIFIED | retain the last accepted snapshot across restart and Central unavailability |
| Configuration failure diagnostics | sync state records attempt/success/error/ETag and existing support diagnostics expose it | CERTIFIED | failures remain observable without deleting accepted state |
| Change-feed machine authentication | puller sends tenant/device/sync-token headers to `/api/v1/sync/changes`; Products/Catalog and Customers E2Es use registered device authority | CERTIFIED | every pull remains tenant/device bound on Central |
| Durable cursor / restart | `sync_checkpoints.central_changes` persists the cursor in SQLite; merged POS #266 certifies restart with retained cursor/config | CERTIFIED | restart resumes from the last successfully saved cursor |
| Cursor advancement after apply | puller advances the checkpoint only after every message on the page applies successfully | CERTIFIED | failed or unsupported change must not advance the cursor |
| Pagination / ordering | puller drains `has_more` pages and Products/Catalog V1 certifies multi-page cursor ordering/replay | CERTIFIED | preserve deterministic ordered catch-up |
| Inbox transactional apply | each change is processed in one SQLite transaction with inbox status plus projection mutation | CERTIFIED | projection and inbox status remain atomic |
| Inbox idempotency / replay | already-applied message IDs return without reapplying; product/customer/catalog E2Es and merged #266/#267 certify duplicate and partial-page replay | CERTIFIED | duplicate delivery remains side-effect free |
| Version monotonicity | category/product/price projections reject older versions; product lifecycle acceptance covers stale/deactivation behavior | CERTIFIED | stale Central facts cannot overwrite newer local projection state |
| Schema compatibility | merged POS #267 rejects `schema_version > 1` before inbox/projection mutation; schema version 0 retains the established V1 default | CERTIFIED | future Central schemas fail closed until the POS runtime explicitly supports them |
| Partial-page recovery | merged POS #267 proves an earlier message may commit while a later unsupported message fails, but the page cursor stays pinned; replay skips the applied message and advances only after full convergence | CERTIFIED | partial application must remain replay-safe and cursor-safe |
| Catalog/product lifecycle convergence | Products/Catalog V1 certifies product/category/barcode/price/GST/deactivation/reassignment/import convergence | CERTIFIED | preserve canonical Central catalog authority and branch applicability |
| Customer canonical convergence | Customers V1 certifies Central ↔ POS identity/lifecycle/financial-snapshot reconciliation | CERTIFIED | Central financial facts remain authoritative; local pending edits remain protected by mapping/version rules |
| Tenant/store isolation | Products/Catalog, Customers, Store/Device and Auth/Az certify server-side tenant/device/branch isolation; merged POS #265 adds client-side config tenant scope binding | CERTIFIED | no Central response may widen tenant/branch/device scope at the edge |
| Revoked-device behavior | Store/Device V1 certifies new config/sync authority fails after revocation while previously accepted config remains available offline | CERTIFIED | no new canonical pulls after revocation; retained state stays read-only/offline |
| Poison/failed inbound diagnostics | failed inbox messages retain identity/type/source/payload/attempt/error; merged #267 keeps unsupported future-schema messages cursor-safe and explicit | CERTIFIED | unsupported/poison messages remain diagnosable without silent projection |
| Reconnect consistency | merged POS #266 proves retained config + ETag + durable change-feed cursor survive SQLite restart and converge newer config/catalog facts after reconnect without duplicate projection | CERTIFIED | stale offline state must converge after reconnect without authority widening |
| Frontend synchronization authority | Frontend Completion evidence certifies packaged local-POS mode disables legacy browser order/customer/inventory/catalog sync workers and keeps POSService as the durable synchronization authority | CERTIFIED | browser code must not become a parallel durable sync engine |
| Cross-repository release acceptance | this release PR runs merged Backend configuration/catalog authority tests, packaged POS test/vet/build plus reconnect/schema acceptance, and merged Frontend sync-authority acceptance/build | CERTIFIED | final release gate must remain green against merged repository heads |

## Release evidence

- POS #265: effective-configuration tenant-scope fail-closed binding.
- POS #266: real-SQLite retained configuration, ETag, cursor, restart, reconnect and idempotent convergence acceptance.
- POS #267: future-schema fail-closed behavior and partial-page replay/cursor safety.
- Existing Products/Catalog, Customers, Store/Device and Auth/Az release evidence: Central device/branch/tenant authority and projection convergence.
- Existing Frontend Completion evidence: packaged local-POS mode does not run a second browser durable synchronization engine.
- `v1-central-pos-sync-release.yml`: final merged-main Backend/POS/Frontend synchronization gate.

## Release decision

**V1 CENTRAL ↔ POS SYNCHRONIZATION RELEASE CERTIFIED.** All matrix rows are certified. Central remains canonical for configuration/catalog/customer authority, POS remains the durable offline projection, and Frontend remains non-authoritative for synchronization. Freeze this domain except for real defects; continue to the next ordered V1 domain.
