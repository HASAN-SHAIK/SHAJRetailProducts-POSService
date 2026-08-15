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
| POS offline customer search | POS #186 proves real SQLite search of active customers by name, phone, email and customer code, including inactive-customer exclusion | CERTIFIED | satisfied by merged POS #186 |
| POS offline customer create/update | POS #186 proves validated transactional create/update, monotonic local versions, durable `customer.changed` emission, inactive lifecycle persistence and fail-closed invalid input | CERTIFIED | satisfied by merged POS #186 |
| POS durable customer outbox | create/update emits versioned `customer.changed` in the same SQLite transaction; POS #182 proves restart/dispatch/replay through Central | CERTIFIED | satisfied by merged POS #182 cross-repo Customer E2E |
| Central customer CRUD/search | Backend #48 proves canonical create/update/list/detail stay within the supplied tenant database and that customer CRUD is tenant-global rather than branch-filtered | CERTIFIED | satisfied by merged Backend #48 |
| POS -> Central customer convergence | Backend #46 maps immutable POS customer IDs to canonical Central IDs without applying POS financial snapshots; POS #182 proves real SQLite -> durable outbox -> restart -> Central/PostgreSQL convergence and replay | CERTIFIED | satisfied by merged Backend #46 + POS #182 |
| Central -> POS customer projection | Backend #47 publishes canonical contact, lifecycle and Central-owned financial snapshots with known POS-local identity mappings; POS #184 proves authenticated Central change feed -> POS inbox/SQLite reconciliation without a duplicate canonical-ID row | CERTIFIED | satisfied by merged Backend #47 + POS #184 |
| Duplicate identity handling | Backend #46/#47 maintain immutable POS-local -> canonical identity mapping and source version; POS #184 reuses a mapped local row only when it exists on that device, preserves a newer pending local edit, and otherwise uses stable canonical identity | CERTIFIED | satisfied in both directions by Backend #46/#47 + POS #182/#184 |
| Sale association | Backend #46 resolves mapped POS customer identity into canonical `orders.customer_id`; POS #182 proves a customer-linked completed sale reaches Central associated to that canonical customer | CERTIFIED | satisfied by merged Backend #46 + POS #182 |
| Credit-limit authority | POS may carry an offline snapshot; Backend #46 never applies POS `credit_limit_minor` to canonical `customers.credit_limit`; POS #184 proves Central's canonical credit-limit snapshot projects back to the reconciled POS row | CERTIFIED | Central remains authoritative; satisfied by Backend #46/#47 + POS #182/#184 |
| Outstanding balance authority | POS may carry an offline snapshot; Backend #46 never applies POS `outstanding_minor` to canonical `customers.current_balance`; POS #184 proves Central's canonical outstanding snapshot projects back to the reconciled POS row | CERTIFIED | Central remains authoritative; satisfied by Backend #46/#47 + POS #182/#184 |
| Customer payments/ledger | Central Customers module exposes payment and ledger behavior, but stored `current_balance` and ledger derivation are not yet one certified projection | PARTIAL | establish one canonical balance mechanism spanning credit sales, payments and returns; certify transactionally |
| Customer lifecycle/deactivation | Backend #47 projects canonical `is_active` state; POS #184 proves an inactive Central customer updates the same mapped offline row and remains excluded by the existing active-only customer search; POS #186 certifies local inactive lifecycle persistence/search exclusion | CERTIFIED | satisfied by Backend #47 + POS #184/#186 |
| Tenant isolation | Backend #48 proves identical customer identifiers in separate tenant pools cannot cross-read or cross-write | CERTIFIED | satisfied by merged Backend #48 |
| Branch/store applicability | Central canonical `customers` is deliberately tenant-global and has no branch/store identity; Backend #48 proves list/create/update/detail do not add branch/store filtering while remaining tenant-pool isolated | CERTIFIED | tenant-global across stores is the V1 policy; satisfied by merged Backend #48 |
| Offline restart/replay | POS #182 proves outbound customer state/outbox survive restart and duplicate delivery converges idempotently; #184 proves inbound canonical reconciliation through the transactional inbox | CERTIFIED | satisfied by merged POS #182/#184 |
| Diagnostics/support visibility | generic outbox/inbox diagnostics exist | PARTIAL | customer-specific failed/dead-letter identity visible to support |
| Frontend customer UX | existing screens/APIs not yet audited against certified POS/Central authority | GAP | cashier create/search/select/error/offline/sync acceptance |

## Ordered V1 work

1. Establish and certify one canonical Central outstanding-balance/ledger mechanism spanning credit sales, customer payments, full returns, partial returns and POS synchronization.
2. Audit and complete customer-specific support diagnostics and cashier/customer Frontend UX.
3. Run final Customers V1 release acceptance; then freeze the domain except for real defects.

## Release decision

Customers V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, with real cross-repository acceptance for the authority boundaries and no unresolved critical issues.
