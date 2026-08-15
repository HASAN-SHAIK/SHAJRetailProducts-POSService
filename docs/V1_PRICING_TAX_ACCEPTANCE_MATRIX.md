# V1 Pricing / Promotions / Tax / Rounding Acceptance Matrix

## Release decision

**V1 PRICING / PROMOTIONS / TAX / ROUNDING RELEASE-CERTIFIED — freeze except for real defects.**

Central Backend is the canonical authority for product base-price facts, tenant tax settings, GST mode, pricing policy, and recovery/configuration. POSService is the offline execution projection: it calculates cashier transactions deterministically from synchronized Central facts and persists the exact price/discount/tax snapshots used for the sale. Frontend is a client only and is not pricing or tax authority.

Transaction Core, Inventory, Products/Catalog, and this Pricing/Tax domain are release-certified and frozen except for real defects.

## Status legend

- **CERTIFIED** — implementation plus focused executable acceptance exists.
- **N/A** — intentionally outside V1 with explicit justification.

## Acceptance matrix

| Capability | Status | Certification evidence |
| --- | --- | --- |
| Central canonical base-price authority | CERTIFIED | Products/Catalog V1 certifies branch-scoped Central `selling_price` transport into the POS catalog projection. |
| POS effective price precedence | CERTIFIED | POSService #170 proves store-specific price beats global, expired/future prices are excluded, priority orders valid prices, and update time is the stable tie-breaker. |
| Effective price restart/replay | CERTIFIED | Products/Catalog V1 proves synchronized price facts survive SQLite restart and replay idempotently. |
| Fractional/weighted quantity price math | CERTIFIED | POSService #165 proves `QuantityMilli` drives fractional price calculation without inferring physical UOM semantics. |
| Canonical UOM/weight interpretation | N/A | V1 cashier arithmetic is quantity-milli based and does not interpret physical UOM labels; adding kg/g/litre conversion semantics would be new product scope. |
| Manual-price permission | CERTIFIED | POSService #169 binds order execution to cached Central `billing.allow_price_override`, defaults closed without a snapshot, and requires product capability plus Central policy. |
| Line discount policy | CERTIFIED | POSService #171 binds offline execution to cached Central `billing.allow_discount` and `billing.max_discount_percent`, fails closed for missing/invalid policy, and persists accepted line snapshots. |
| Order discount policy | N/A | The certified V1 offline POS contract has no order-level discount field; line discounts are the V1 contract. |
| Promotions/campaigns | N/A | Repository audit found no canonical campaign/coupon engine in the three V1 repositories; adding one would be scope expansion. |
| Product HSN/GST-rate authority | CERTIFIED | Backend #44 transports canonical `hsn_code` and `gst_percentage`; POSService #173 persists and validates those facts in SQLite. |
| Tenant GST enabled/disabled policy | CERTIFIED | Backend #43 establishes `tax.gst_enabled`; POSService #174 consumes it and deterministically produces zero tax when disabled. |
| GST inclusive/exclusive mode | CERTIFIED | Backend #43 establishes `tax.gst_mode`; POSService #174 certifies deterministic `INCLUSIVE` and `EXCLUSIVE` offline execution. |
| POS deterministic tax calculation | CERTIFIED | POSService #174 replaces caller tax authority in the packaged runtime with Central-policy/product-rate-driven GST after line discount. |
| Central preservation/reconciliation of POS tax snapshot | CERTIFIED | POSService #175 proves non-zero immutable price/discount/GST/tax-code snapshots survive durable outbox delivery, Central PostgreSQL projection, and duplicate replay without recalculation. |
| Monetary rounding rule | CERTIFIED | Backend #43 establishes V1 `HALF_UP`; POSService #174 certifies the minor-unit HALF_UP boundary. |
| Refund tax/discount reversal | CERTIFIED | POSService #176 proves full refund reverses the immutable taxed/discounted captured total instead of recalculating tax. |
| Partial-return tax/discount reversal | CERTIFIED | POSService #176 proves cumulative proportional returns converge exactly to the immutable original line total, including final rounding remainder. |
| Branch/store price isolation | CERTIFIED | Products/Catalog V1 certifies active-device branch isolation for synchronized price facts. |
| Tenant price/tax isolation | CERTIFIED | Backend #45 certifies tenant + registered-device/branch isolation for effective pricing/tax configuration, distinct provenance/ETags, and rejection of unregistered devices where registration is required. |
| Offline pricing availability | CERTIFIED | POS persists effective prices and Central manual-price, discount, GST-enabled, GST-mode, and rounding policies locally; #174 proves deterministic execution from the cached snapshot. |
| Pricing/tax diagnostics | CERTIFIED | POSService #179 exposes persisted effective-config last attempt, last success, last error, and ETag through existing read-only support diagnostics; exact-head Pricing, POS edge/integration, Refund, and Partial Return gates passed. |

## Final release acceptance

The release gate requires every non-N/A matrix row to be CERTIFIED and preserves these architectural invariants:

1. Central remains the canonical pricing/tax/configuration authority.
2. POS remains deterministic and offline-capable from synchronized durable facts.
3. Frontend cannot supply authoritative price, discount, or tax decisions.
4. Completed sale price/discount/tax facts are immutable snapshots and Central preserves them rather than silently recalculating them.
5. Refunds and partial returns reverse those immutable snapshots without rounding leakage.
6. Support diagnostics remain read-only and do not create POS recovery or pricing mutation authority.

All non-N/A rows are CERTIFIED. Pricing / Promotions / Tax / Rounding V1 is therefore release-certified pending this exact-head release gate and is frozen after merge except for real defects. The ordered next domain is **Customers V1**.
