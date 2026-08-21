# Order / Customer / Staff Identity Authority Matrix

Status: initial audit baseline

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

| Relationship | Canonical field | Required? | Authority | Historical snapshot | Initial POS finding |
| --- | --- | --- | --- | --- | --- |
| Order | `order_id` / `client_order_id` | yes | POS creates local transaction identity; Central projects idempotently | no | Present in `orders.CreateInput` / `Order` |
| Store | `store_id` | yes | Central configuration/permission scope | optional store-name snapshot | Present and persisted |
| Terminal | `terminal_id` / device identity | expected for POS | POS installation identity registered with Central | optional terminal label | `terminal_id` exists on order input/model; generation/validation still needs tracing |
| Customer | `customer_id` | nullable for anonymous sale | Central customer identity, with deliberate offline-local identity strategy where needed | yes: name/contact fields used on receipt/reporting as transaction-time context | POS validates customer against local `customers` projection and persists `customer_id`; snapshot mapping not yet present in core order model |
| Creator/operator | `created_by_user_id` | yes for authenticated POS order | authenticated POS session mapped to canonical Central user/staff identity | yes where audit display must remain stable | SQLite column exists, but core `Order` / `CreateInput` does not carry or persist it |
| Completer/cashier | `completed_by_user_id` | yes when sale is completed | authenticated POS session at completion | yes where audit display must remain stable | SQLite column exists, but `Complete` / `CompleteWith` does not populate it |
| Payment actor | `created_by_user_id` on payment | yes | authenticated POS session | optional actor snapshot | SQLite column exists; propagation still needs tracing |
| Receipt issuer | `issued_by_user_id` | yes | completion/receipt session | optional actor snapshot | SQLite column exists; propagation still needs tracing |
| Approver | approval record / `approved_by_user_id` contract | conditional | Central-authorized manager identity, validated locally via approval token/session | approval-time identity/name/role context | Manager approval schema exists; order/refund/void linkage still needs tracing |
| Tenant | tenant identity | yes | Central | no | Must never be inferred from customer/staff; trace configuration and event envelope |
| Sync identity | event/outbox id + aggregate id + version | yes | POS transactional outbox | immutable event payload | Must be atomic with order mutation and idempotent at Central; trace pending |

## Confirmed breakpoints on current POS `main`

1. `internal/database/migrations/0010_order_cashier_audit.sql` adds `created_by_user_id` and `completed_by_user_id` to `sales_orders`, plus actor fields on payments and receipts.
2. `internal/orders/orders.go` `CreateInput` does not expose an authenticated actor identity and `Order` does not model `CreatedByUserID` or `CompletedByUserID`.
3. The `sales_orders` INSERT in `orders.Service.Create` omits both cashier-audit columns, so the schema capability is not used by the core order creation flow.
4. `orders.Service.Complete` and `CompleteWith` update completion timestamp/version but do not set `completed_by_user_id`.
5. Customer mapping is partially stronger: `customer_id` is accepted as a flexible external ID, validated against the local customer projection, persisted on `sales_orders`, and surfaced on the order model. However, no customer transaction-time snapshot is represented in the core `Order` model, so historical display can become dependent on mutable customer master data unless another layer supplies a snapshot.

## Dependency-safe implementation order

1. Trace local authentication/session identity and HTTP order handlers to find the authoritative user/staff ID source.
2. Trace customer selection/creation in Frontend and POS customer projection, including offline-created customers and anonymous sales.
3. Trace order outbox/event payloads and Central projection to locate fields currently lost between SQLite and PostgreSQL.
4. Lock the canonical contract for actor/customer/store/terminal/tenant/event identities.
5. Add actor fields to the POS order domain and persistence without trusting arbitrary frontend-supplied staff IDs; actor identity must come from the authenticated POS session/context.
6. Add completion/payment/receipt actor propagation in the same atomic business transaction.
7. Add transaction-time customer/staff snapshots only for fields required for historical receipts/reporting; do not duplicate mutable master data unnecessarily.
8. Update outbox/event schemas and Central projections idempotently.
9. Fix Frontend mapping only after the POS/Central contract is explicit.
10. Certify registered customer, anonymous customer, offline customer creation, creator vs completer, manager approval, offline sync, duplicate event, lost acknowledgement, deactivated staff, changed customer/staff data, tenant/store isolation, refund/void linkage.

## Dashboard migration coordination

RetailHub/dashboard work must consume the canonical Central relationships and must not invent customer/staff mapping or duplicate attribution formulas. Before modifying overlapping order/reporting files, inspect the dashboard-migration PR/branch state and defer conflicting edits.

## Next audit slice

Trace the authenticated POS session/user identity source, order HTTP handlers, customer frontend request payload, transactional outbox event schema, and Central order projection. Convert every unknown row above into a confirmed source/persistence/serialization/projection/query path before widening code changes.
