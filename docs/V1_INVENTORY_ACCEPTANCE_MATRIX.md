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
- The merged cross-repository inventory acceptance covers SQLite persistence across restart, durable outbox reconnect, duplicate delivery, lost acknowledgement, and canonical PostgreSQL convergence.

### Central

- `pos_inventory_movements` records immutable POS-originated movement identity.
- Canonical application is guarded by `canonical_applied_at`, so replaying the same movement cannot apply a second stock delta.
- Canonical product stock is updated atomically with movement application and missing canonical products fail closed rather than silently acknowledging divergence.
- Purchase creation increments `products.stock_quantity` directly and updates/creates batch quantities inside the purchase transaction; purchase audit/receiving acceptance remains to be completed.

## Acceptance matrix

| Capability | Required V1 acceptance | Current status |
| --- | --- | --- |
| Canonical stock authority | Central PostgreSQL holds canonical branch/store stock truth; POS balance is an offline projection | CERTIFIED — merged Backend canonical movement application plus cross-repo PostgreSQL acceptance establish Central as canonical authority for POS-originated inventory effects |
| Purchase/receiving | Central purchase receipt increases canonical stock exactly once, including batch quantity where enabled | PARTIAL — direct transactional increment exists; canonical movement/audit acceptance still required |
| Sale decrement | Frontend sale -> POS SQLite decrement/movement -> durable outbox -> Central applies exactly once | CERTIFIED — real SQLite -> durable outbox -> Central/PostgreSQL E2E asserts one canonical decrement |
| Offline sale | POS can decrement local stock while Central is unreachable and later converge without duplicate decrement | CERTIFIED — restart/reconnect acceptance proves durable local decrement and later canonical convergence |
| Full refund restoration | POS restoration and Central canonical stock converge exactly once | CERTIFIED — full-refund E2E asserts Central stock restoration after durable refund/inventory replay |
| Partial refund/return restoration | Only returned quantity is restored locally and centrally, exactly once | CERTIFIED — partial-return E2E asserts only returned quantity is restored in PostgreSQL |
| Pre-completion void | No completed inventory issue survives a void before completion | NEEDS ACCEPTANCE |
| Duplicate delivery | Replayed inventory event cannot apply a second stock delta | CERTIFIED — immutable movement identity plus canonical application marker prevents duplicate effect |
| Lost acknowledgement | Central commit followed by lost acknowledgement/retry still results in one canonical delta | CERTIFIED — cross-repo inventory/refund acceptance replays after committed-but-unacknowledged delivery and asserts one canonical effect |
| Crash/restart | SQLite balance, movement, and pending outbox survive restart and resume convergence | CERTIFIED — inventory E2E restarts POSService before reconnect and proves durable continuation |
| Dead-letter | Poison inventory event is visible and cannot be silently discarded | NEEDS ACCEPTANCE |
| Central-authorized recovery | Replay/skip/recovery decisions remain Central-authorized; no Frontend/POS force-correction authority | NEEDS ACCEPTANCE |
| Exactly-once convergence | Local movement set and Central canonical movement set converge to one logical effect per movement id | CERTIFIED — movement id is immutable and `canonical_applied_at` gates canonical stock mutation exactly once |
| Inventory audit history | Central exposes durable reason/source/reference for purchase, sale, refund/return, and administrative adjustment | PARTIAL — POS movement history is durable; Central purchase/adjustment audit mapping still needs acceptance |
| Manual adjustment | Authorized Central operation records reason/actor and updates canonical stock atomically | NEEDS ACCEPTANCE |
| Store/branch isolation | Inventory mutation/read cannot cross tenant or branch/store authority boundary | NEEDS ACCEPTANCE |
| Negative stock / oversell | V1 policy is explicit and consistently enforced online/offline; no accidental policy emerges from implementation | GAP — policy/acceptance not yet explicit |
| Batch inventory | Batch-enabled products reconcile product total and batch remaining quantity across receiving/sale/return | PARTIAL — receiving support exists; end-to-end reconciliation acceptance required |
| Cashier/operator visibility | Cashier can distinguish local available stock, pending Central convergence, blocked sync, and recovered/synced state where relevant | NEEDS ACCEPTANCE |
| Reconciliation | Support/admin can compare POS movement IDs/balances with Central canonical facts and identify divergence | GAP |

## Release criteria

Inventory V1 is release-certified only when all matrix rows are either `CERTIFIED` or explicitly documented `N/A` for V1 with a reason. Certification must include focused unit/integration coverage plus real cross-repository POS SQLite -> durable outbox -> Central/PostgreSQL inventory E2E coverage for sale decrement, reconnect convergence, replay safety, and refund restoration.

No additional manager-approval semantics should be added unless inventory acceptance exposes an actual authorization defect.

## Ordered implementation priorities

1. Certify pre-completion void, poison/dead-letter visibility, and Central-authorized recovery without expanding transaction semantics.
2. Certify purchase receiving, manual adjustment/audit, branch/store isolation, and batch reconciliation.
3. Make negative-stock/oversell policy explicit and test the same policy online/offline.
4. Add cashier/operator and support reconciliation visibility.
5. Run final Inventory V1 release certification and freeze the domain except for defects.

Transaction Core V1 is frozen except for defects discovered by this matrix.
