# SHAJRetailProducts V1 Central ↔ POS Synchronization Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is canonical for tenant, branch, device registration, effective configuration, products/categories/prices/tax facts, canonical customer financial/lifecycle snapshots, and recovery authority.
- POSService/SQLite is the durable offline edge projection. It may retain the last accepted Central state while disconnected, but it must not widen tenant/branch/device scope or invent canonical configuration/catalog truth.
- Frontend is presentation/orchestration only and must not become a second configuration/catalog synchronization engine.
- Outbound POS business facts continue through the already-certified durable outbox. This matrix focuses on Central → POS configuration/catalog/customer projection and reconnect consistency.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| Packaged runtime wiring | `cmd/posservice` starts the change-feed puller and effective-config refresh service when Central is configured | CERTIFIED | preserve the packaged runtime as the production sync composition root |
| Effective configuration machine authentication | config client sends tenant/device/sync-token headers and requires HTTPS outside loopback; Store/Device V1 certifies active Central registration on the server side | CERTIFIED | retain active-device Central authority and secure transport |
| Effective configuration scope binding | client rejects a mismatched returned device ID, but does not yet reject a mismatched returned tenant scope | GAP | fail closed on tenant/device scope mismatch before persisting a snapshot |
| Effective configuration precedence | Pricing/Tax and Store/Device evidence certifies Central tenant → branch → device effective-policy resolution | CERTIFIED | preserve Central precedence and source provenance |
| ETag / 304 refresh | client sends `If-None-Match`; service treats 304 as unchanged success and persists sync state | CERTIFIED | no-op refresh must not replace the accepted snapshot |
| Durable configuration snapshot / restart | SQLite `effective_config_snapshot` stores schema, ETag, tenant/branch/device scope and payload; restart/offline-retention acceptance already exists | CERTIFIED | retain the last accepted snapshot across restart and Central unavailability |
| Configuration failure diagnostics | sync state records attempt/success/error/ETag and existing support diagnostics expose it | CERTIFIED | failures remain observable without deleting accepted state |
| Change-feed machine authentication | puller sends tenant/device/sync-token headers to `/api/v1/sync/changes`; Products/Catalog and Customers E2Es use registered device authority | CERTIFIED | every pull remains tenant/device bound on Central |
| Durable cursor / restart | `sync_checkpoints.central_changes` persists the cursor in SQLite | CERTIFIED | restart resumes from the last successfully saved cursor |
| Cursor advancement after apply | puller advances the checkpoint only after every message on the page applies successfully | CERTIFIED | failed change must not advance the cursor |
| Pagination / ordering | puller drains `has_more` pages and Products/Catalog V1 certifies multi-page cursor ordering/replay | CERTIFIED | preserve deterministic ordered catch-up |
| Inbox transactional apply | each change is processed in one SQLite transaction with inbox status plus projection mutation | CERTIFIED | projection and inbox status remain atomic |
| Inbox idempotency / replay | already-applied message IDs return without reapplying; product/customer/catalog E2Es certify duplicate replay | CERTIFIED | duplicate delivery remains side-effect free |
| Version monotonicity | category/product/price projections reject older versions; product lifecycle acceptance covers stale/deactivation behavior | CERTIFIED | stale Central facts cannot overwrite newer local projection state |
| Catalog/product lifecycle convergence | Products/Catalog V1 certifies product/category/barcode/price/GST/deactivation/reassignment/import convergence | CERTIFIED | preserve canonical Central catalog authority and branch applicability |
| Customer canonical convergence | Customers V1 certifies Central ↔ POS identity/lifecycle/financial-snapshot reconciliation | CERTIFIED | Central financial facts remain authoritative; local pending edits remain protected by mapping/version rules |
| Tenant/store isolation | Products/Catalog, Customers, Store/Device and Auth/Az certify tenant/device/branch isolation, but config response tenant scope is not yet checked client-side | PARTIAL | close config tenant-scope mismatch and retain server-side branch/device isolation |
| Revoked-device behavior | Store/Device V1 certifies new config/sync authority fails after revocation while previously accepted config remains available offline | CERTIFIED | no new canonical pulls after revocation; retained state stays read-only/offline |
| Poison/failed inbound diagnostics | failed inbox messages retain identity/type/source/payload/attempt/error and existing diagnostics expose catalog/customer failures | CERTIFIED | unsupported/poison messages remain diagnosable and cursor-safe |
| Reconnect consistency | existing config refresh + change-feed workers restart/retry independently; final combined reconnect acceptance is not yet explicit | PARTIAL | prove stale offline config/catalog converges after reconnect without duplicate projection or authority widening |
| Cross-repository release acceptance | extensive catalog/customer/config domain E2Es exist, but there is no single Central↔POS sync release gate yet | PARTIAL | merged Backend + packaged POS SQLite acceptance for config ETag/restart, catalog/customer cursor replay, isolation and reconnect |

## First ordered work

1. Fail closed when effective configuration returns a tenant scope different from the configured Central tenant; add focused client acceptance.
2. Add combined reconnect acceptance covering retained configuration + durable change-feed cursor + transactional inbox replay.
3. Audit Central change-feed/config response bounds/schema compatibility and poison-message recovery behavior.
4. Add the final merged-main Central↔POS synchronization release gate and mark every row CERTIFIED or explicitly justified N/A.

## Release decision

**NOT YET RELEASE CERTIFIED.** Existing configuration, catalog and customer synchronization capabilities are substantial and should be reused. The remaining work is scope hardening plus combined reconnect/release evidence, not a new synchronization subsystem.
