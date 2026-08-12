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

### Central

- Product rows currently expose mutable `stock_quantity`.
- Purchase creation increments `products.stock_quantity` directly and updates/creates batch quantities inside the purchase transaction.
- Before V1 certification, Central inventory authority must be proven as an auditable, idempotent convergence path for POS-originated movements and Central-originated receipts/adjustments rather than inferred from product CRUD state alone.

## Acceptance matrix

| Capability | Required V1 acceptance | Current status |
| --- | --- | --- |
| Canonical stock authority | Central PostgreSQL holds canonical branch/store stock truth; POS balance is an offline projection | GAP — authority model not yet release-certified |
| Purchase/receiving | Central purchase receipt increases canonical stock exactly once, including batch quantity where enabled | PARTIAL — direct transactional increment exists; canonical movement/audit acceptance still required |
| Sale decrement | Frontend sale -> POS SQLite decrement/movement -> durable outbox -> Central applies exactly once | PARTIAL — POS side certified by transaction core; Central stock convergence needs explicit inventory acceptance |
| Offline sale | POS can decrement local stock while Central is unreachable and later converge without duplicate decrement | PARTIAL — edge transaction/outbox certified; inventory-specific Central convergence needs explicit test |
| Full refund restoration | POS restoration and Central canonical stock converge exactly once | PARTIAL — POS/refund path exists; inventory-specific Central assertion required |
| Partial refund/return restoration | Only returned quantity is restored locally and centrally, exactly once | PARTIAL — POS path tested; Central canonical assertion required |
| Pre-completion void | No completed inventory issue survives a void before completion | NEEDS ACCEPTANCE |
| Duplicate delivery | Replayed inventory event cannot apply a second stock delta | NEEDS ACCEPTANCE |
| Lost acknowledgement | Central commit followed by lost acknowledgement/retry still results in one canonical delta | NEEDS ACCEPTANCE |
| Crash/restart | SQLite balance, movement, and pending outbox survive restart and resume convergence | NEEDS INVENTORY-SPECIFIC ACCEPTANCE |
| Dead-letter | Poison inventory event is visible and cannot be silently discarded | NEEDS ACCEPTANCE |
| Central-authorized recovery | Replay/skip/recovery decisions remain Central-authorized; no Frontend/POS force-correction authority | NEEDS ACCEPTANCE |
| Exactly-once convergence | Local movement set and Central canonical movement set converge to one logical effect per movement id | GAP — explicit canonical movement/idempotency contract required |
| Inventory audit history | Central exposes durable reason/source/reference for purchase, sale, refund/return, and administrative adjustment | PARTIAL — existing stock audit support must be mapped and certified |
| Manual adjustment | Authorized Central operation records reason/actor and updates canonical stock atomically | NEEDS ACCEPTANCE |
| Store/branch isolation | Inventory mutation/read cannot cross tenant or branch/store authority boundary | NEEDS ACCEPTANCE |
| Negative stock / oversell | V1 policy is explicit and consistently enforced online/offline; no accidental policy emerges from implementation | GAP — policy/acceptance not yet explicit |
| Batch inventory | Batch-enabled products reconcile product total and batch remaining quantity across receiving/sale/return | PARTIAL — receiving support exists; end-to-end reconciliation acceptance required |
| Cashier/operator visibility | Cashier can distinguish local available stock, pending Central convergence, blocked sync, and recovered/synced state where relevant | NEEDS ACCEPTANCE |
| Reconciliation | Support/admin can compare POS movement IDs/balances with Central canonical facts and identify divergence | GAP |

## Release criteria

Inventory V1 is release-certified only when all matrix rows are either `CERTIFIED` or explicitly documented `N/A` for V1 with a reason. Certification must include focused unit/integration coverage plus at least one real cross-repository Frontend/POS SQLite -> durable outbox -> Central/PostgreSQL inventory E2E covering sale decrement and reconnect convergence.

No additional manager-approval semantics should be added unless inventory acceptance exposes an actual authorization defect.

## Ordered implementation priorities

1. Establish Central canonical inventory movement/idempotency contract keyed by immutable movement/event identity.
2. Prove normal + offline POS sale decrement convergence into PostgreSQL exactly once, including duplicate delivery and lost acknowledgement.
3. Prove full/partial refund restoration and pre-completion void inventory semantics.
4. Certify purchase receiving, manual adjustment/audit, branch/store isolation, and batch reconciliation.
5. Make negative-stock/oversell policy explicit and test the same policy online/offline.
6. Add operator/support reconciliation visibility and final Inventory V1 release certification.

Transaction Core V1 is frozen except for defects discovered by this matrix.