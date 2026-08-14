# V1 Pricing / Promotions / Tax / Rounding Acceptance Matrix

## Authority and scope

Central Backend is the canonical authority for product base-price facts, tenant tax settings, GST mode, pricing/promotion policy, and recovery/configuration. POSService is the offline execution projection: it must calculate a cashier transaction deterministically from synchronized Central facts and persist the exact price/discount/tax snapshots used for the sale. Frontend is a client only and must not become pricing or tax authority.

Transaction Core, Inventory, and Products/Catalog V1 are release-certified and frozen except for real defects.

## Status legend

- **CERTIFIED** — implementation plus focused executable acceptance exists.
- **PARTIAL** — capability exists but V1 authority/fidelity or end-to-end evidence is incomplete.
- **GAP** — a real release gap is identified.
- **N/A** — intentionally outside V1 or unsupported with explicit justification.

## Acceptance matrix

| Capability | Status | Current evidence / required acceptance |
| --- | --- | --- |
| Central canonical base-price authority | CERTIFIED | Products/Catalog V1 certifies branch-scoped Central `selling_price` transport into the POS catalog projection. |
| POS effective price precedence | CERTIFIED | POSService #170 proves store-specific price beats global, expired/future prices are excluded, priority orders valid prices, and update time is the stable tie-breaker. |
| Effective price restart/replay | CERTIFIED | Products/Catalog V1 proves synchronized price facts survive SQLite restart and replay idempotently. |
| Fractional/weighted quantity price math | CERTIFIED | POSService #165 proves `QuantityMilli` drives fractional price calculation without inferring physical UOM semantics. |
| Canonical UOM/weight interpretation | PARTIAL | Central owns `is_weight_based`; the catalog feed still emits placeholder `unit`. Pricing V1 must define the minimum authoritative weight/UOM rule needed for cashier validation without inventing POS-only semantics. |
| Manual-price permission | CERTIFIED | POSService #169 binds actual POS order execution to cached Central `billing.allow_price_override`, defaults closed without a snapshot, and requires both product capability and Central policy for a true override. |
| Line discount policy | CERTIFIED | POSService #171 binds offline POS execution to cached Central `billing.allow_discount` and `billing.max_discount_percent`, defaults discounts closed without an authoritative snapshot, rejects malformed/out-of-range policy, and persists only accepted line-discount snapshots. POS-to-Central snapshot reconciliation remains tracked separately below. |
| Order discount policy | N/A | The certified V1 offline POS order contract has no order-level discount field; discounts are line-scoped immutable snapshots. Central's separate legacy online billing path is not the Transaction Core authority. Do not add a second offline order-discount mechanism without an explicit V1 product requirement. |
| Promotions/campaigns | GAP | No canonical V1 promotion engine has yet been established. Inspect existing capabilities before deciding the minimum V1 promotion scope. |
| Product HSN/GST-rate authority | PARTIAL | Central products store `hsn_code` and `gst_percentage`, but the current POS catalog feed does not transport an authoritative equivalent. |
| Tenant GST enabled/disabled policy | PARTIAL | Central sale logic supports GST enablement via payload/plan features, but POS-effective configuration and offline execution are not yet certified. |
| GST inclusive/exclusive mode | PARTIAL | Central tenant/application settings support `INCLUSIVE`/`EXCLUSIVE`; the POS price feed currently hardcodes `tax_inclusive=true`. |
| POS deterministic tax calculation | GAP | POS currently accepts caller-supplied `TaxMinor`; there is no certified Central-fact-driven offline GST calculation yet. |
| Central preservation/reconciliation of POS tax snapshot | GAP | POS order items persist `tax_minor`, but Central `sale.completed` ingestion currently drops the line tax snapshot and hardcodes GST enabled. |
| Monetary rounding rule | GAP | POS gross arithmetic currently uses integer `price * quantity_milli / 1000`; an explicit minor-unit rounding rule and cross-runtime parity are not yet certified. |
| Refund tax/discount reversal | PARTIAL | Transaction/Inventory refund paths are certified, but exact price/discount/tax reversal parity has not yet been accepted in this domain. |
| Partial-return tax/discount reversal | PARTIAL | Partial-return mechanics are certified, but proportional tax/discount rounding and snapshot parity are not yet certified. |
| Branch/store price isolation | CERTIFIED | Products/Catalog V1 certifies active-device branch isolation for synchronized price facts. |
| Tenant price/tax isolation | PARTIAL | Catalog tenant isolation is certified; tenant tax settings and pricing-policy isolation need Pricing V1 acceptance. |
| Offline pricing availability | PARTIAL | POS has local price facts plus cached manual-price and line-discount policy; tax/promotion policy is not yet fully synchronized into deterministic execution. |
| Pricing/tax diagnostics | GAP | Operator/support evidence for stale/failed pricing-policy or tax configuration has not yet been certified. |

## Ordered closure work

1. Establish explicit GST-enabled, GST-mode, product-rate/HSN, and rounding authorities in Central and transport the minimum versioned facts Central -> POS.
2. Make POS calculate and persist deterministic tax snapshots offline from those facts; do not delegate authority to Frontend.
3. Preserve/reconcile immutable price/discount/tax snapshots through Central ingestion, refunds and partial returns.
4. Establish the minimum V1 promotion scope only after auditing existing implementation; do not invent a separate campaign subsystem if none is required for V1.
5. Add tenant isolation, failure diagnostics, and final cross-repository Pricing V1 release acceptance.

## Release rule

Pricing / Promotions / Tax / Rounding V1 is release-certified only when every non-N/A row is CERTIFIED or explicitly moved to a later V1 domain with architecture justification and executable boundary acceptance. Once certified, freeze this domain except for real defects and proceed to Customers V1.
