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
| Canonical UOM/weight interpretation | N/A | V1 cashier arithmetic is explicitly quantity-milli based and does not interpret physical UOM labels. Products/Catalog owns product identity/weight facts; adding kg/g/litre conversion semantics would be a new product capability rather than a Pricing V1 release requirement. |
| Manual-price permission | CERTIFIED | POSService #169 binds actual POS order execution to cached Central `billing.allow_price_override`, defaults closed without a snapshot, and requires both product capability and Central policy for a true override. |
| Line discount policy | CERTIFIED | POSService #171 binds offline POS execution to cached Central `billing.allow_discount` and `billing.max_discount_percent`, defaults discounts closed without an authoritative snapshot, rejects malformed/out-of-range policy, and persists only accepted line-discount snapshots. |
| Order discount policy | N/A | The certified V1 offline POS order contract has no order-level discount field; discounts are line-scoped immutable snapshots. Central's separate legacy online billing path is not the Transaction Core authority. Do not add a second offline order-discount mechanism without an explicit V1 product requirement. |
| Promotions/campaigns | N/A | Repository audit found no canonical promotion/campaign/coupon contract or engine in the three V1 repositories. V1 has explicit line-discount authority; inventing a separate campaign subsystem would be product-scope expansion. |
| Product HSN/GST-rate authority | CERTIFIED | Backend #44 transports canonical `hsn_code` and `gst_percentage`; POSService #173 persists and validates the projected GST facts in SQLite. |
| Tenant GST enabled/disabled policy | CERTIFIED | Backend #43 establishes `tax.gst_enabled`; POSService #174 consumes the cached Central policy and deterministically produces zero tax when disabled. |
| GST inclusive/exclusive mode | CERTIFIED | Backend #43 establishes `tax.gst_mode`; POSService #174 certifies deterministic `INCLUSIVE` and `EXCLUSIVE` offline execution. |
| POS deterministic tax calculation | CERTIFIED | POSService #174 replaces caller tax authority in the packaged runtime with Central-policy/product-rate-driven GST calculation after line discount. |
| Central preservation/reconciliation of POS tax snapshot | CERTIFIED | POSService #175 proves non-zero immutable price/discount/GST/tax-code snapshots survive durable outbox delivery, Central ingestion, PostgreSQL projection, and duplicate replay without recalculation. |
| Monetary rounding rule | CERTIFIED | Backend #43 establishes V1 `HALF_UP`; POSService #174 certifies the minor-unit HALF_UP boundary in offline GST calculation. |
| Refund tax/discount reversal | CERTIFIED | POSService #176 proves full refund reverses the immutable taxed/discounted captured sale total instead of recalculating tax. |
| Partial-return tax/discount reversal | CERTIFIED | POSService #176 proves cumulative proportional returns against immutable `line_total_minor` converge exactly to the original line total, including the final rounding remainder. |
| Branch/store price isolation | CERTIFIED | Products/Catalog V1 certifies active-device branch isolation for synchronized price facts. |
| Tenant price/tax isolation | PARTIAL | Catalog tenant isolation is certified; effective tax/pricing-policy isolation still needs focused tenant/device acceptance. |
| Offline pricing availability | CERTIFIED | POS persists effective price facts and Central manual-price, discount, GST-enabled, GST-mode and rounding policies locally; #174 proves deterministic calculation from the cached snapshot in the packaged runtime. |
| Pricing/tax diagnostics | GAP | Effective-config sync state persists last attempt, last success, last error and ETag, but focused operator/support exposure and acceptance are not yet certified. |

## Ordered closure work

1. Certify tenant/device isolation for effective pricing/tax configuration.
2. Expose and certify read-only pricing/tax effective-config synchronization diagnostics using the existing persisted sync state; do not add correction authority to POS or Frontend.
3. Run final cross-repository Pricing V1 release acceptance against merged Backend/POS main.

## Release rule

Pricing / Promotions / Tax / Rounding V1 is release-certified only when every non-N/A row is CERTIFIED or explicitly moved to a later V1 domain with architecture justification and executable boundary acceptance. Once certified, freeze this domain except for real defects and proceed to Customers V1.
