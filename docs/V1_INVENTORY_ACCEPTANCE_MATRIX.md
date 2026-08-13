# V1 Inventory Acceptance Matrix

## Architecture authority

- Central Backend/PostgreSQL is the canonical authority for inventory truth, recovery, administrative adjustments, store/branch scope, and durable cross-device convergence.
- POSService/SQLite is the small offline edge runtime. It may maintain local balances and append immutable inventory movements needed for offline selling, but it must not become an independent canonical inventory authority.
- Frontend is an operator UI. It reads local POS inventory/status while offline and Central inventory/admin views where appropriate; it must not invent recovery or stock-correction authority.
- Every externally replayable inventory fact must be idempotent. Duplicate delivery, restart, lost acknowledgement, and retry must converge without double decrement/restoration.

## Current implementation evidence

### POS edge

- `internal/inventory/inventory.go` maintains per-store balances and append-only movement rows.
- Sale completion issues a `sale_issue` movement once per order item and appends an inventory outbox event in the same SQLite transaction.
- Full refund and partial return implementations restore inventory with focused tests.
- Pre-completion void acceptance proves an unpaid order can be cancelled without creating any inventory movement.
- Sync diagnostics expose pending/failed/dead-letter outbox events with event identity, ordering key, attempts, last error, payload, and metadata; focused inventory acceptance verifies poison inventory events remain operator/support-visible.
- Central-authorized recovery is single-use, audited, ordering-aware, and cannot be replayed after consumption; POS/Frontend do not gain force-correction authority.
- The merged cross-repository inventory acceptance covers SQLite persistence across restart, durable outbox reconnect, duplicate delivery, lost acknowledgement, and canonical PostgreSQL convergence.
- POSService #141 explicitly certifies the V1 provisional-negative policy: without an authoritative Central inventory initialization marker, POS must not reject a tracked sale solely because local stock truth is absent/stale. The edge records the provisional negative balance, immutable sale movement, and durable inventory outbox fact for Central convergence.

### Central

- `pos_inventory_movements` records immutable POS-originated movement identity.
- Canonical application is guarded by `canonical_applied_at`, so replaying the same movement cannot apply a second stock delta.
- Canonical product stock is updated atomically with movement application and missing canonical products fail closed rather than silently acknowledging divergence.
- Purchase creation increments `products.stock_quantity` and batch quantities inside the same transaction. Backend #26 adds focused acceptance that receiving establishes stock-audit context before mutation, locks the canonical product, preserves supplier/batch branch scope, increments batch and canonical quantities transactionally, rolls back on failure, and carries actor/reason/source/reference into the database audit context.
- Backend #27 adds the Central/admin-only manual-adjustment path with required reason/branch/non-zero delta, canonical product locking, audit context before mutation, negative-stock rejection, batch/product atomicity, rollback, and branch/batch ownership enforcement.
- Backend #28 fixes stock-read branch isolation by resolving the caller's branch authority before query execution: restricted staff are pinned to their assigned branch, while privileged callers retain explicit-branch and all-branch reads. Focused acceptance executes these cases without requiring a live PostgreSQL connection.

## Acceptance matrix

| Capability | Required V1 acceptance | Current status |
| --- | --- | --- |
| Canonical stock authority | Central PostgreSQL holds canonical branch/store stock truth; POS balance is an offline projection | CERTIFIED — merged Backend canonical movement application plus cross-repo PostgreSQL acceptance establish Central as canonical authority for POS-originated inventory effects |
| Purchase/receiving | Central purchase receipt increases canonical stock exactly once, including batch quantity where enabled | CERTIFIED — Backend #26 verifies transactional receiving, product locking, batch and canonical quantity increments, rollback behavior, branch scoping, and purchase stock-audit context |
| Sale decrement | Frontend sale -> POS SQLite decrement/movement -> durable outbox -> Central applies exactly once | CERTIFIED — real SQLite -> durable outbox -> Central/PostgreSQL E2E asserts one canonical decrement |
| Offline sale | POS can decrement local stock while Central is unreachable and later converge without duplicate decrement | CERTIFIED — restart/reconnect acceptance proves durable local decrement and later canonical convergence |
| Full refund restoration | POS restoration and Central canonical stock converge exactly once | CERTIFIED — full-refund E2E asserts Central stock restoration after durable refund/inventory replay |
| Partial refund/return restoration | Only returned quantity is restored locally and centrally, exactly once | CERTIFIED — partial-return E2E asserts only returned quantity is restored in PostgreSQL |
| Pre-completion void | No completed inventory issue survives a void before completion | CERTIFIED — focused void acceptance asserts zero inventory movements for a valid unpaid pre-completion void |
| Duplicate delivery | Replayed inventory event cannot apply a second stock delta | CERTIFIED — immutable movement identity plus canonical application marker prevents duplicate effect |
| Lost acknowledgement | Central commit followed by lost acknowledgement/retry still results in one canonical delta | CERTIFIED — cross-repo inventory/refund acceptance replays after committed-but-unacknowledged delivery and asserts one canonical effect |
| Crash/restart | SQLite balance, movement, and pending outbox survive restart and resume convergence | CERTIFIED — inventory E2E restarts POSService before reconnect and proves durable continuation |
| Dead-letter | Poison inventory event is visible and cannot be silently discarded | CERTIFIED — sync diagnostics retain dead-letter inventory identity, ordering key, attempts, error, payload, and metadata; ordering logic blocks later same-key events rather than silently skipping the poison event |
| Central-authorized recovery | Replay/skip/recovery decisions remain Central-authorized; no Frontend/POS force-correction authority | CERTIFIED — existing recovery acceptance proves single-use Central authorization, durable audit, ordering safety, and replay rejection |
| Exactly-once convergence | Local movement set and Central canonical movement set converge to one logical effect per movement id | CERTIFIED — movement id is immutable and `canonical_applied_at` gates canonical stock mutation exactly once |
| Inventory audit history | Central exposes durable reason/source/reference for purchase, sale, refund/return, and administrative adjustment | PARTIAL — purchase and manual-adjustment audit context are acceptance-certified; unified sale/refund audit mapping still needs acceptance |
| Manual adjustment | Authorized Central operation records reason/actor and updates canonical stock atomically | CERTIFIED — Backend #27 provides the Central/admin-only audited adjustment path with atomic canonical/batch mutation, rollback, branch/batch scope, and negative-stock rejection |
| Store/branch isolation | Inventory mutation/read cannot cross tenant or branch/store authority boundary | CERTIFIED — existing mutation paths enforce branch ownership and Backend #28 fixes/acceptance-certifies restricted stock reads while preserving privileged explicit/all-branch behavior |
| Negative stock / oversell | V1 policy is explicit and consistently enforced online/offline; no accidental policy emerges from implementation | CERTIFIED — POSService #141 makes the existing architecture explicit: when POS has no authoritative initialized stock truth, tracked sales may create a provisional negative local balance online/offline, but the sale movement and durable outbox fact are mandatory so Central remains canonical and reconciliation can surface divergence; hard edge blocking is deferred until authoritative Central -> POS inventory initialization exists |
| Batch inventory | Batch-enabled products reconcile product total and batch remaining quantity across receiving/sale/return | PARTIAL — receiving support is certified; end-to-end sale/return reconciliation acceptance required |
| Cashier/operator visibility | Cashier can distinguish local available stock, pending Central convergence, blocked sync, and recovered/synced state where relevant | NEEDS ACCEPTANCE |
| Reconciliation | Support/admin can compare POS movement IDs/balances with Central canonical facts and identify divergence | GAP |

## Release criteria

Inventory V1 is release-certified only when all matrix rows are either `CERTIFIED` or explicitly documented `N/A` for V1 with a reason. Certification must include focused unit/integration coverage plus real cross-repository POS SQLite -> durable outbox -> Central/PostgreSQL inventory E2E coverage for sale decrement, reconnect convergence, replay safety, and refund restoration.

No additional manager-approval semantics should be added unless inventory acceptance exposes an actual authorization defect.

## Ordered implementation priorities

1. Certify batch reconciliation across receiving, sale, full refund, and partial return.
2. Add cashier/operator and support reconciliation visibility, and close the remaining unified audit mapping.
3. Run final Inventory V1 release certification and freeze the domain except for defects.

Transaction Core V1 is frozen except for defects discovered by this matrix.
