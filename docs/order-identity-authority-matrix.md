# Order / Customer / Staff Identity Authority Matrix

Status: audit baseline expanded; authenticated actor source and creator/completer persistence path confirmed

## Goal

Establish one durable identity contract across Frontend -> POS Service -> SQLite -> transactional outbox -> Central Backend -> PostgreSQL so every transaction can answer:

- who bought it,
- who created/processed/completed it,
- who approved exceptional actions,
- where it happened,
- which terminal/device created it,
- and which event/version synchronized it.

IDs establish relationships. Transaction-time snapshots preserve historical context.

## Canonical relationship model

| Relationship | Canonical field | Required? | Authority | Historical snapshot | Current POS finding |
| --- | --- | --- | --- | --- | --- |
| Order | `order_id` / `client_order_id` | yes | POS creates local transaction identity; Central projects idempotently | no | Present in `orders.CreateInput` / `Order` |
| Store | `store_id` | yes | registered POS device + Central configuration/permission scope | optional store-name snapshot | `handleOrderCreate` overwrites request store with registered device store; frontend cannot choose arbitrary store |
| Terminal | `terminal_id` / device identity | expected for POS | registered POS installation identity | optional terminal label | `handleOrderCreate` injects registered device terminal into order input |
| Customer | `customer_id` | nullable for anonymous sale | Central customer identity, with deliberate offline-local identity strategy where needed | yes: receipt/reporting fields required to remain historically stable | POS validates customer against local `customers` projection and persists `customer_id`; snapshot mapping still needs end-to-end tracing |
| Creator/operator | `created_by_user_id` | yes for authenticated POS order | authenticated POS session mapped to canonical Central user/staff identity | yes where audit display must remain stable | Authoritative `user_id` is already available in request context, but creator is currently written after the order transaction via `recordOrderCreator`, so creation + creator attribution are not atomic |
| Completer/cashier | `completed_by_user_id` | yes when sale is completed | authenticated POS session at completion | yes where audit display must remain stable | `cashierCompletionAuditHook` writes `completed_by_user_id` inside `CompleteWith` transaction and patches the `sale.completed` outbox payload actor; core `Order` model/snapshot still does not expose actor fields |
| Payment actor | `created_by_user_id` on payment | yes | authenticated POS session | optional actor snapshot | server helper `recordPaymentCreator` exists; atomicity/event propagation still needs tracing |
| Receipt issuer | `issued_by_user_id` | yes | authenticated completion/receipt session | optional actor snapshot | completion audit hook writes receipt issuer inside sale-completion transaction |
| Approver | approval record / `approved_by_user_id` contract | conditional | manager approval token/session validated locally against Central-authorized grant | approval-time identity/name/role context | Manager approval subsystem is present; order/refund/void linkage remains a separate audit slice |
| Tenant | tenant identity | yes | Central grant/configuration | no | Local auth `User` and `LocalUserContext` carry `tenant_id`; it must stay independent from customer/store/staff inference |
| Sync identity | event/outbox id + aggregate id + version | yes | POS transactional outbox | immutable event payload | Completion hook targets `sales_order` + aggregate id + version + `sale.completed`; creation-event path still needs tracing |

## Confirmed authenticated actor path

1. Central offline grant contains canonical `user_id`, `tenant_id`, role, branch and permissions.
2. POS `localauth.Service` verifies/enrolls that grant and persists the canonical user identity in `local_users`.
3. POS login creates a local session token associated with that canonical user.
4. `localAuthMiddleware` authenticates `X-POS-Session-Token` and stores `LocalUserContext{UserID,TenantID,Role,BranchID,...}` on the HTTP request context.
5. Order handlers therefore already have a trustworthy server-side actor source and do not need a frontend-supplied staff id.
6. `cashierFromRequest` explicitly ignores the internal-test fallback and returns the authenticated actor for real POS requests.

## Confirmed breakpoints on current POS `main`

1. `internal/database/migrations/0010_order_cashier_audit.sql` adds `created_by_user_id` and `completed_by_user_id` to `sales_orders`, plus actor fields on payments and receipts.
2. `internal/orders/orders.go` `CreateInput` and `Order` do not model creator/completer identities.
3. The `sales_orders` INSERT in `orders.Service.Create` omits both cashier-audit columns.
4. `handleOrderCreate` first commits `orders.Service.Create`, then separately calls `recordOrderCreator` using a standalone `UPDATE`. A crash/database error between those operations can leave a durable order with no creator attribution.
5. Because `saveSnapshot` executes inside `orders.Service.Create` before `recordOrderCreator`, the version-1 order snapshot cannot contain creator identity even when the later audit update succeeds.
6. `CompleteWith` is architecturally stronger: it supports hooks in the same SQLite transaction. `cashierCompletionAuditHook` writes `completed_by_user_id`, receipt issuer, and `sale.completed` actor metadata atomically with completion/outbox work.
7. However, the in-memory/core `Order` model still omits creator/completer fields, so snapshots/read APIs cannot return those identities consistently.
8. Customer mapping is partially stronger: `customer_id` accepts Central numeric or POS string IDs, validates against the local projection, persists on `sales_orders`, and surfaces on `Order`. No transaction-time customer snapshot is represented in the core order model, so historical display can still depend on mutable customer master data unless another layer supplies a snapshot.

## Required creator fix

The next POS implementation slice must make creator attribution part of the same order-create transaction. The server must derive creator identity from `LocalUserContext`, never from request JSON. The order domain/read model should expose `created_by_user_id` (and subsequently `completed_by_user_id`) so snapshots and outbox payloads can carry stable actor relationships.

Acceptance for this slice:

- authenticated order creation persists `created_by_user_id` in the same SQLite commit as `sales_orders` and its version-1 snapshot;
- anonymous/internal test fallback does not fabricate a production staff identity;
- request JSON cannot override `created_by_user_id`;
- duplicate `client_order_id` returns the existing order without changing its original creator;
- a read of the created order returns the persisted creator identity;
- existing registered/anonymous customer behavior remains unchanged.

## Dependency-safe implementation order

1. Implement atomic creator attribution from authenticated request context into the POS order transaction and version-1 snapshot.
2. Promote creator/completer fields into the core `Order` read model and ensure `CompleteWith` snapshots include the completer identity rather than only the database column/outbox patch.
3. Trace customer selection/creation in Frontend and POS customer projection, including anonymous and offline-created customers.
4. Trace creation/completion outbox payloads and Central projection to locate fields lost between SQLite and PostgreSQL.
5. Lock the final cross-repo contract for actor/customer/store/terminal/tenant/event identities.
6. Add payment/receipt actor propagation guarantees where any remaining update is outside the atomic business transaction.
7. Add transaction-time customer/staff snapshots only for fields required for historical receipts/reporting; do not duplicate mutable master data unnecessarily.
8. Update Central projections idempotently and add relational indexes/validation as needed.
9. Fix Frontend mapping only where the confirmed POS/Central contract requires it.
10. Certify registered customer, anonymous customer, offline customer creation, creator vs completer, manager approval, offline sync, duplicate event, lost acknowledgement, deactivated staff, changed customer/staff data, tenant/store isolation, refund/void linkage.

## Dashboard migration coordination

The active RetailHub dashboard migration is currently isolated in `SHAJRetailProducts-CustomerHub` and consumes Central reporting APIs. This audit/identity PR changes only POSService documentation, so there is no file-level conflict in the current slice. Future Central reporting changes must continue to inspect dashboard PRs first. RetailHub must consume canonical Central customer/staff relationships and must not invent attribution or duplicate formulas.

## Next audit / implementation slice

Implement and test atomic `created_by_user_id` persistence in the order-create transaction, then trace the customer frontend payload and Central `sale.completed` projection before widening to customer snapshots or reporting changes.