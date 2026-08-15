# SHAJRetailProducts V1 Customers Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central Backend/PostgreSQL is canonical for customer identity, lifecycle, credit/outstanding balance, ledger/payment history, recovery and tenant/branch authority.
- POSService/SQLite is the small offline edge projection used for cashier lookup and permitted offline customer capture/update.
- POS-originated customer facts must converge durably and idempotently to Central. Central-originated canonical customer changes must project back to POS before this domain is release-certified.
- Frontend is never the canonical customer or credit authority.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| POS offline customer search | SQLite repository searches active customers by name, phone, email and customer code | PARTIAL | focused restart/offline acceptance |
| POS offline customer create/update | SQLite repository validates and persists customer facts transactionally | PARTIAL | focused local lifecycle acceptance |
| POS durable customer outbox | create/update emits versioned `customer.changed` in the same SQLite transaction; POS #182 proves restart/dispatch/replay through Central | CERTIFIED | satisfied by merged POS #182 cross-repo Customer E2E |
| Central customer CRUD/search | existing Central Customers module owns canonical create/update/list/detail behavior | PARTIAL | focused authority/isolation acceptance |
| POS -> Central customer convergence | Backend #46 maps immutable POS customer IDs to canonical Central IDs without applying POS financial snapshots; POS #182 proves real SQLite -> durable outbox -> restart -> Central/PostgreSQL convergence and replay | CERTIFIED | satisfied by merged Backend #46 + POS #182 |
| Central -> POS customer projection | Central emits `customer.upsert`, but duplicate-safe reconciliation of canonical identity back into an offline `cus_*` row is not yet certified | GAP | authenticated Central change -> POS inbox/SQLite E2E without duplicate local customer |
| Duplicate identity handling | Backend #46 persists immutable `pos_customer_id -> canonical_customer_id` mapping and #182 proves duplicate replay remains one canonical customer | CERTIFIED | satisfied for POS -> Central; Central -> POS local-row reconciliation remains covered by the projection row |
| Sale association | Backend #46 resolves mapped POS customer identity into canonical `orders.customer_id`; POS #182 proves a customer-linked completed sale reaches Central associated to that canonical customer | CERTIFIED | satisfied by merged Backend #46 + POS #182 |
| Credit-limit authority | POS may carry an offline snapshot, while Backend #46 deliberately never applies POS `credit_limit_minor` to canonical `customers.credit_limit`; #182 verifies Central remains unchanged | CERTIFIED | Central remains authoritative; refresh of Central snapshot to POS is covered by the projection row |
| Outstanding balance authority | POS may carry an offline snapshot, while Backend #46 never applies POS `outstanding_minor` to canonical `customers.current_balance`; #182 verifies Central remains authoritative | CERTIFIED | Central remains authoritative; refresh of Central snapshot to POS is covered by the projection row |
| Customer payments/ledger | Central Customers module exposes payment and ledger behavior | PARTIAL | transactional ledger/payment acceptance; POS scope decision |
| Customer lifecycle/deactivation | POS search excludes inactive customers; Central has `is_active` lifecycle | PARTIAL | Central deactivate/reactivate -> POS projection acceptance |
| Tenant isolation | tenant DB boundary exists in Central | PARTIAL | explicit cross-tenant customer isolation acceptance |
| Branch/store applicability | not yet certified for customer visibility/creation | GAP | define V1 global-vs-branch customer policy and test device/store behavior |
| Offline restart/replay | POS #182 proves customer state/outbox survive restart and duplicate delivery converges idempotently through Central/PostgreSQL | CERTIFIED | satisfied by merged POS #182 |
| Diagnostics/support visibility | generic outbox/inbox diagnostics exist | PARTIAL | customer-specific failed/dead-letter identity visible to support |
| Frontend customer UX | existing screens/APIs not yet audited against certified POS/Central authority | GAP | cashier create/search/select/error/offline/sync acceptance |

## Ordered V1 work

1. Establish duplicate-safe Central -> POS canonical customer projection, preserving the offline local identity while refreshing Central-owned lifecycle and financial snapshots.
2. Certify Central deactivate/reactivate and contact/tax updates through authenticated change feed -> POS inbox/SQLite.
3. Certify customer payments/ledger authority and decide the minimum POS scope for credit/outstanding snapshots.
4. Certify tenant and branch/store visibility policy.
5. Audit and complete cashier/customer Frontend UX and customer-specific support diagnostics.
6. Run final Customers V1 release acceptance; then freeze the domain except for real defects.

## Release decision

Customers V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, with real cross-repository acceptance for the authority boundaries and no unresolved critical issues.
