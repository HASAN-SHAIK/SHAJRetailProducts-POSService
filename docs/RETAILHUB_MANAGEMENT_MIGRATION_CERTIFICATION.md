# RetailHub Management Migration Certification

## Decision

SHAJ Retail Hub (`HASAN-SHAIK/SHAJRetailProducts-CustomerHub`) is the business-management/control-plane application. POS remains the store execution/offline edge. Central/PostgreSQL remains canonical authority for management data and business reporting.

## Certified ownership matrix

| Domain | RetailHub / Central authority | POS retained responsibility | Certification state |
| --- | --- | --- | --- |
| Dashboard analytics | Sales/revenue, profit/growth, inventory analytics, products/categories, customer credit, branch performance, smart insights | Device health, connectivity, sync/outbox/dead-letter and local operational state | Complete |
| Customers | Canonical customer directory, profiles, credit/admin views and history | Checkout-time search/select plus architecture-approved lightweight offline capture and durable sync | Complete |
| Staff | Staff profiles, branch assignment, activation/deactivation and management | Current logged-in operator identity and runtime permission enforcement | Complete |
| Expenses | Canonical expense management and reporting | Genuine store/register execution only | Complete |
| Accounts | Receipt/payment administration, cash/bank books, ledger, outstanding and opening setup management | Opening-completion safety gate plus genuine store/register execution | Complete |

## POS retirement boundary

The POS application SHALL NOT advertise or implement canonical Customers, Staff, Expenses or Accounts management. Compatibility routes may redirect/handoff to store execution or RetailHub, but must not restore management API authority.

POS SHALL retain:

- retail/wholesale billing and order execution;
- checkout customer lookup/select and approved offline customer capture;
- authenticated operator identity and permission enforcement;
- returns/corrections needed for store execution;
- inventory/store execution paths approved for POS;
- Sync Center, outbox/dead-letter diagnostics, connectivity/device health;
- the fail-closed opening-completion gate, with management performed in RetailHub.

## Release acceptance

Release certification requires all jobs in `.github/workflows/retailhub-management-migration-release.yml` to pass against merged `main` of the four repositories:

1. POSService complete Go test/vet/build.
2. Backend complete Jest suite to revalidate canonical authority, tenant isolation and management APIs.
3. Frontend focused RetailHub-retirement acceptance plus production build.
4. RetailHub dashboard/customers/staff/expenses/accounts migration acceptance plus production build.

The certification is invalid if any migrated management domain regains POS-local canonical authority or if the preserved offline/store execution boundaries regress.
