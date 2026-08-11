# V1 Transaction Acceptance Matrix

This matrix is the release authority for the SHAJRetailProducts V1 transaction core across Frontend -> POS SQLite -> durable outbox -> Central Backend/PostgreSQL.

## Architecture boundary

- Central remains identity, permission, recovery authorization, canonical state, and idempotency authority.
- POS remains the small offline edge runtime responsible for local transaction durability, SQLite state, one-time capability enforcement, outbox ordering, retry, and reconciliation facts.
- Frontend remains operator UX/orchestration and must not become an identity, permission, recovery, or canonical data authority.

## Release rule

A row is `CERTIFIED` only when there is a focused automated release signal or an explicit cross-repository E2E signal on the exact PR head. Package-wide tests are supporting evidence, but they do not by themselves promote a row to `CERTIFIED`.

| Journey / invariant | V1 status | Current release evidence | Next action if not certified |
| --- | --- | --- | --- |
| Normal sale | CERTIFIED | `TestOrderVerticalSlicePersistsAcrossSQLiteRestart`; Cross-repo Order E2E exact-head signal | Keep frozen unless a defect is found |
| Offline sale | CERTIFIED | Local sale/outbox durability is proven by `TestOrderVerticalSlicePersistsAcrossSQLiteRestart`; Cross-repo Order E2E closes/reopens the committed POS SQLite file before reconnect and Central dispatch | Keep frozen unless a defect is found |
| Payment | CERTIFIED | Cross-repo Order E2E dispatches the real POS outbox through the production Central sync route and asserts exactly one `pos_sale_payments` projection after replay | Keep frozen unless a defect is found |
| Receipt | CERTIFIED | `TestCompletedSaleCommitsReceiptInventoryAndOutboxTogether` proves the completion response and POS receipt read model expose the same immutable receipt number/hash; Cross-repo Receipt E2E proves durable independent Central delivery and idempotent replay | Keep frozen unless a defect is found |
| Pre-completion void | CERTIFIED | `TestManagerApprovedVoidConsumesApprovalAndSurvivesRestart`; V1 edge acceptance | Keep frozen unless a defect is found |
| Full refund | CERTIFIED | `TestManagerApprovedRefundConsumesApprovalAndSurvivesRestart`; Cross-repo Refund E2E | Keep frozen unless a defect is found |
| Partial refund / return | CERTIFIED | `TestPartialRefundOrderScopedApprovalLifecycleAcrossRestart`; Cross-repo Partial Return E2E | Keep frozen unless a defect is found |
| Manager approval | CERTIFIED | Exact scope, single-use concurrency, restart, wrong-order and wrong-cashier certifications | Freeze approval semantics unless a real defect is found |
| Inventory effects | CERTIFIED | Normal-sale local decrement is covered by V1 edge acceptance; Cross-repo Order E2E asserts exactly one `pos_inventory_movements` projection after replay; void/refund/return effects are separately certified | Keep frozen unless a defect is found |
| Crash / restart | CERTIFIED | Void/refund/recovery/lost-ack restart certifications; normal-sale SQLite restart durability | Keep frozen unless a defect is found |
| Offline -> online sync | CERTIFIED | Cross-repo Order E2E persists the pending normal-sale outbox in SQLite, closes/reopens POS storage, rebuilds the production sync engine, reconnects to Central, and verifies PostgreSQL convergence plus idempotent replay | Keep frozen unless a defect is found |
| Duplicate delivery | CERTIFIED | Cross-repo Order E2E replays the same normal-sale sync event and asserts a single processed event and single canonical projections | Keep frozen unless a defect is found |
| Lost acknowledgement | CERTIFIED | `TestRefundEventRecoversWhenCentralAcceptsButAcknowledgementIsLost`; `TestV1RefundSyncLostAckRestartAndOrderingAcceptance` | Keep frozen unless a defect is found |
| Dead-letter handling | CERTIFIED | Central-authorized recovery plus reconciliation visibility | Keep frozen unless a defect is found |
| Central-authorized recovery | CERTIFIED | `TestCentralAuthorizedRecoverySurvivesRestartAndPreservesOrderHead` | Keep Central as recovery authority |
| Exactly-once Central convergence | CERTIFIED | Normal-sale Cross-repo Order E2E asserts one processed sync event plus one sale, payment, receipt, inventory movement, and canonical order after replay; refund/partial-return cross-repo E2E and lost-ack paths provide additional coverage | Keep frozen unless a defect is found |
| Cashier/operator status | PARTIAL | Refund reconciliation snapshot is explicit | Add Frontend -> POS operator-state acceptance for sale/sync blocked/recovered/synced states |

## Current V1 release focus

Manager approval and refund/void semantics are frozen unless an actual defect is discovered. Remaining transaction-core work should proceed in this order:

1. Frontend operator-visible local/sync/recovery states.
2. Final cross-repository transaction-core certification.

When all rows are `CERTIFIED`, the transaction core is release-certified and work should move to the next V1 Retail OS domain rather than expanding manager-approval micro-tests.
