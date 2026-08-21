# Order / Customer / Staff Identity Authority Matrix

Status: core order/staff identity path implemented; customer lifecycle and exceptional-action certification in progress

## Canonical relationship model

| Relationship | Canonical field | Authority | Current status |
| --- | --- | --- | --- |
| Order | `order_id` / `client_order_id` | POS transaction identity, idempotently projected by Central | confirmed |
| Store | `store_id` | registered POS device + Central configuration | confirmed server-side |
| Terminal | `terminal_id` | registered POS installation | confirmed server-side |
| Customer | `customer_id` | Central customer identity or deliberate POS-local offline identity | active + anonymous behavior certified; offline reconciliation remains |
| Customer history | immutable receipt customer snapshot | transaction-time receipt | confirmed existing receipt snapshot path; Central persistence already exists |
| Creator/operator | `created_by_user_id` | authenticated POS session / canonical Central user | implemented atomically on order creation and version-1 snapshot |
| Completer/cashier | `completed_by_user_id` | authenticated POS session at completion | implemented consistently in model, SQLite, snapshot and completion outbox |
| Receipt issuer | `issued_by_user_id` | authenticated completion session | confirmed |
| Approver | `approver_user_id` on manager approval / action records | authenticated manager verified locally from Central-authorized grant | manager/cashier separation, tenant/branch scope and one-time token binding confirmed; final order/refund/void persistence certification in progress |
| Tenant | `tenant_id` | Central grant/configuration | confirmed independent authority |
| Branch/store scope | `branch_id` / store identity | Central grant + registered device | manager approval scope checks confirmed |
| Sync identity | outbox event id + aggregate id + version | transactional outbox | confirmed for `sale.completed`; broader retry certification remains |

## Confirmed trusted actor path

1. Central offline grant contains canonical `user_id`, `tenant_id`, role, branch and permissions.
2. POS persists the canonical user and authenticates a local session.
3. Middleware places `LocalUserContext{UserID,TenantID,Role,BranchID,...}` on the request.
4. Order creation derives creator identity from that context and never trusts request JSON for staff identity.
5. Completion derives completer/cashier identity from the authenticated session.
6. Store and terminal are injected from the registered POS device instead of arbitrary frontend values.

## Implemented order/staff fixes

- `created_by_user_id` is written in the initial `sales_orders` INSERT in the same SQLite transaction as items and version-1 snapshot.
- `created_by_user_id` and `completed_by_user_id` are part of the canonical POS `Order` model/read path.
- Idempotent `client_order_id` retry preserves the original creator and cannot be used by a second cashier to replace attribution.
- Completion attaches the authenticated completer before hooks execute, persists it in the completion transaction and stores it in the completion snapshot.
- Creator and completer remain distinct when different staff perform those actions.
- `sale.completed` carries creator/completer and authenticated actor metadata into the transactional outbox.
- Central projection already prefers explicit source creator/completer identities and retains fallback behavior only for older events.

## Customer identity status

### Certified

- active local customer ID survives order creation and order read-back;
- anonymous sale keeps `customer_id` null rather than creating a synthetic customer;
- local offline customer create/update is durable and produces versioned `customer.changed` outbox events;
- inactive customers are excluded from normal POS customer search;
- transaction-time customer details are already preserved through immutable receipt snapshot data rather than requiring a second mutable order-customer copy.

### Confirmed gap

Order creation currently checks that a referenced customer row exists but does not yet require `status <> 'inactive'`. A caller that already knows an inactive customer ID can therefore attach it directly. This must be rejected at the order boundary with focused coverage without changing anonymous-sale behavior or customer sync semantics.

## Manager approval / refund / void identity status

The existing approval subsystem already preserves separate identities and does not infer one actor from another:

- requesting cashier identity comes from the authenticated POS session;
- manager identity is separately authenticated with manager credentials;
- self-approval is explicitly forbidden;
- manager tenant must equal cashier tenant;
- manager branch must match the cashier branch unless the manager has all-branch access;
- approval is bound to requesting cashier, permission and—where required—order ID and refund action scope;
- approval token is one-time and expiry-bound;
- void/refund handlers use the verified manager `ApproverUserID` when cashier lacks direct permission;
- direct-permission actions use the authenticated acting user instead of fabricating a manager identity.

Existing focused tests already cover cashier binding, order/action scope, restart durability and refund-approval issuance. The remaining identity work here is to certify the persisted/outbox/Central actor relationship for completed void/refund operations.

## CI state

Core POS suites are green on the current identity head: POS integration, authentication/authorization, POS edge, pricing/tax, reporting/category and refund approval issuance.

Cross-repo Refund E2E and Partial Return E2E currently fail before refund identity assertions because Backend main hits a duplicate `pos_inventory_batch_allocations (movement_id, allocation_seq)` key already pre-seeded by the workflow. This is an unrelated inventory fixture/idempotency issue and is intentionally not being mixed into this identity PR.

## Remaining dependency-safe work

1. Reject inactive/non-selectable customer IDs at the order-create boundary and certify active/inactive/anonymous behavior.
2. Certify registered/offline-created customer -> receipt snapshot -> outbox -> Central customer mapping/reconciliation.
3. Certify refund/void actor and manager approver persistence through POS SQLite/outbox and Central projection.
4. Certify historical identity behavior after customer/staff updates or staff deactivation.
5. Certify duplicate delivery, lost acknowledgement, restart/retry and tenant/store isolation for the identity contract.
6. Run final Frontend -> POS -> SQLite -> outbox -> Central Backend -> PostgreSQL release certification.

## Dashboard coordination

The active dashboard migration is isolated in `SHAJRetailProducts-CustomerHub` and currently has a Branch Performance PR. This identity PR changes POSService only. RetailHub must continue consuming canonical Central reporting/customer/staff relationships; it must not invent attribution or duplicate identity mapping logic.
