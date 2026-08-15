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
| Central -> POS customer projection | Backend #47 publishes canonical contact, lifecycle and Central-owned financial snapshots with known POS-local identity mappings; POS #184 proves authenticated Central change feed -> POS inbox/SQLite reconciliation without a duplicate canonical-ID row | CERTIFIED | satisfied by merged Backend #47 + POS #184 |
| Duplicate identity handling | Backend #46/#47 maintain immutable POS-local -> canonical identity mapping and source version; POS #184 reuses a mapped local row only when it exists on that device, preserves a newer pending local edit, and otherwise uses stable canonical identity | CERTIFIED | satisfied in both directions by Backend #46/#47 + POS #182/#184 |
| Sale association | Backend #46 resolves mapped POS customer identity into canonical `orders.customer_id`; POS #182 proves a customer-linked completed sale reaches Central associated to that canonical customer | CERTIFIED | satisfied by merged Backend #46 + POS #182 |
| Credit-limit authority | POS may carry an offline snapshot; Backend #46 never applies POS `credit_limit_minor` to canonical `customers.credit_limit`; POS #184 proves Central's canonical credit-limit snapshot projects back to the reconciled POS row | CERTIFIED | Central remains authoritative; satisfied by Backend #46/#47 + POS #182/#184 |
| Outstanding balance authority | POS may carry an offline snapshot; Backend #46 never applies POS `outstanding_minor` to canonical `customers.current_balance`; POS #184 proves Central's canonical outstanding snapshot projects back to the reconciled POS row | CERTIFIED | Central remains authoritative; satisfied by Backend #46/#47 + POS #182/#184 |
| Customer payments/ledger | Central Customers module exposes payment and ledger behavior | PARTIAL | transactional ledger/payment acceptance; POS scope decision |
| Customer lifecycle/deactivation | Backend #47 projects canonical `is_active` state; POS #184 proves an inactive Central customer updates the same mapped offline row and remains excluded by the existing active-only customer search | CERTIFIED | satisfied by Backend #47 + POS #184; explicit reactivation may be covered by focused lifecycle acceptance if not already implicit in upsert semantics |
| Tenant isolation | tenant DB boundary exists in Central | PARTIAL | explicit cross-tenant customer isolation acceptance |
| Branch/store applicability | not yet certified for customer visibility/creation | GAP | define V1 global-vs-branch customer policy and test device/store behavior |
| Offline restart/replay | POS #182 proves outbound customer state/outbox survive restart and duplicate delivery converges idempotently; #184 proves inbound canonical reconciliation through the transactional inbox | CERTIFIED | satisfied by merged POS #182/#184 |
| Diagnostics/support visibility | generic outbox/inbox diagnostics exist | PARTIAL | customer-specific failed/dead-letter identity visible to support |
| Frontend customer UX | existing screens/APIs not yet audited against certified POS/Central authority | GAP | cashier create/search/select/error/offline/sync acceptance |

## Ordered V1 work

1. Certify focused POS offline search/create/update lifecycle behavior now that bidirectional identity convergence is closed.
2. Certify Central customer CRUD plus payments/ledger authority and decide the minimum POS scope for financial snapshots.
3. Define and certify tenant-global vs branch/store customer visibility/creation policy with explicit isolation acceptance.
4. Audit and complete cashier/customer Frontend UX and customer-specific support diagnostics.
5. Run final Customers V1 release acceptance; then freeze the domain except for real defects.

## Release decision

Customers V1 is release-certified only when every row above is **CERTIFIED** or explicitly justified **N/A**, with real cross-repository acceptance for the authority boundaries and no unresolved critical issues.
