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
| Offline sale | NEEDS EXPLICIT SIGNAL | Offline/outbox behavior is covered indirectly by package tests | Add explicit offline sale -> restart -> reconnect -> Central convergence acceptance |
| Payment | NEEDS EXPLICIT SIGNAL | Supporting package coverage only | Add focused payment durability and exactly-once Central projection acceptance |
| Receipt | NEEDS EXPLICIT SIGNAL | No explicit V1 release signal in current edge workflow | Add focused completed-sale receipt/read-model acceptance without adding authority to Frontend |
| Pre-completion void | CERTIFIED | `TestManagerApprovedVoidConsumesApprovalAndSurvivesRestart`; V1 edge acceptance | Keep frozen unless a defect is found |
| Full refund | CERTIFIED | `TestManagerApprovedRefundConsumesApprovalAndSurvivesRestart`; Cross-repo Refund E2E | Keep frozen unless a defect is found |
| Partial refund / return | CERTIFIED | `TestPartialRefundOrderScopedApprovalLifecycleAcrossRestart`; Cross-repo Partial Return E2E | Keep frozen unless a defect is found |
| Manager approval | CERTIFIED | Exact scope, single-use concurrency, restart, wrong-order and wrong-cashier certifications | Freeze approval semantics unless a real defect is found |
| Inventory effects | PARTIAL | Void no-compensation and refund/return paths are certified; normal-sale local decrement is covered by the normal-sale release signal | Add explicit normal-sale Central inventory convergence signal |
| Crash / restart | CERTIFIED | Void/refund/recovery/lost-ack restart certifications; normal-sale SQLite restart durability | Keep frozen unless a defect is found |
| Offline -> online sync | PARTIAL | Refund lost-ack/restart/order sync is explicit | Add explicit normal-sale reconnect/convergence signal |
| Duplicate delivery | NEEDS EXPLICIT SIGNAL | Supporting idempotency coverage exists outside this release matrix | Promote exact duplicate delivery / Central idempotency test to V1 release signal |
| Lost acknowledgement | CERTIFIED | `TestRefundEventRecoversWhenCentralAcceptsButAcknowledgementIsLost`; `TestV1RefundSyncLostAckRestartAndOrderingAcceptance` | Keep frozen unless a defect is found |
| Dead-letter handling | CERTIFIED | Central-authorized recovery plus reconciliation visibility | Keep frozen unless a defect is found |
| Central-authorized recovery | CERTIFIED | `TestCentralAuthorizedRecoverySurvivesRestartAndPreservesOrderHead` | Keep Central as recovery authority |
| Exactly-once Central convergence | PARTIAL | Refund/partial-return cross-repo E2E, lost-ack idempotency paths, and normal-sale Cross-repo Order E2E | Add explicit standalone normal-sale payment/inventory convergence signal |
| Cashier/operator status | PARTIAL | Refund reconciliation snapshot is explicit | Add Frontend -> POS operator-state acceptance for sale/sync blocked/recovered/synced states |

## Current V1 release focus

Manager approval and refund/void semantics are frozen unless an actual defect is discovered. Remaining transaction-core work should proceed in this order:

1. Payment + inventory standalone Central convergence for a normal sale.
2. Offline sale + SQLite restart + reconnect + durable outbox convergence.
3. Duplicate delivery and exactly-once Central handling for the normal sale/payment path.
4. Receipt/read-model acceptance for a completed sale.
5. Frontend operator-visible local/sync/recovery states.
6. Final cross-repository transaction-core certification.

When all rows are `CERTIFIED`, the transaction core is release-certified and work should move to the next V1 Retail OS domain rather than expanding manager-approval micro-tests.
