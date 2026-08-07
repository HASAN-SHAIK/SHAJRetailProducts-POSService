# Stacked implementation plan

Each branch builds on the previous branch and should be reviewed as a small, isolated change.

1. `agent/00-pos-foundation`
2. `agent/01-sqlite-database`
3. `agent/02-device-and-store-identity`
4. `agent/03-catalog-module`
5. `agent/04-customer-module`
6. `agent/05-order-module`
7. `agent/06-payment-module`
8. `agent/07-inventory-module`
9. `agent/08-receipt-module`
10. `agent/09-transactional-outbox`
11. `agent/10-sync-engine`
12. `agent/11-inbox-and-change-feed`
13. `agent/12-security-and-auth`
14. `agent/13-backup-and-recovery`
15. `agent/14-observability`
16. `agent/15-packaging-and-installer`
17. `agent/16-frontend-local-api-adapter`
18. `agent/17-central-sync-ingestion`
19. `agent/18-integration-tests`

The frontend adapter branch must not change visual components, styling, navigation, or operator workflows. It may only add repository adapters, configuration, and feature flags.
