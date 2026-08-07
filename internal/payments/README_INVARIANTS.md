# Payments invariants

- `client_payment_id` is the idempotency key for payment creation.
- Replaying the same `client_payment_id` with the same normalized payment payload returns the existing payment without changing order state or version.
- Reusing the same `client_payment_id` for a different order or a conflicting payment payload is rejected.
- Payment creation and order payment-state recalculation commit in one SQLite transaction.
- Captured inbound payments increase paid amount; captured/refunded outbound payments reduce paid amount.
- Order payment status is derived as `confirmed`, `partially_paid`, or `paid` from the effective paid amount.
- Invalid payment requests must not insert a payment or mutate the order version.
