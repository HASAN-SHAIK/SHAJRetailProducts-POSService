# SHAJRetailProducts V1 Deployment Runbook

This runbook orders production deployment around the already-certified Security, Database/Recovery, Central↔POS Sync and Observability authorities.

## Preconditions

- Release only exact reviewed Git heads. POS artifacts must retain `RELEASE-MANIFEST.txt` and a verified `SHA256SUMS.txt`.
- Verify current Central native backup/recovery readiness and a recent verified POS SQLite backup.
- Keep production secrets in deployment/provider secret stores or the protected POS environment file; never in source, Frontend bundles, command lines, logs or diagnostics.
- Record non-secret release identifiers: Backend commit, POS commit/version/manifest commit, Frontend commit.

## Ordered deployment

1. **Central Backend first.** Apply the audited tenant migration path, deploy the production non-root container, and require `/health` plus PostgreSQL-aware `/ready` to be healthy before continuing. A partial tenant migration is a failed deployment; use the Database V1 forward-fix policy rather than guessing rollback SQL.
2. **POS Service second.** Verify the incoming checksum/manifest, stop the service, replace only binary/package files, preserve SQLite/device/local-token/config/backup state, start the service and require local health/readiness. Allow durable outbox/inbox/config change-feed replay to converge before declaring stores healthy.
3. **Frontend static artifact last.** Deploy only the reviewed production static/container artifact after Central and POS authority are healthy. The Frontend carries no Central secret and must not be used to repair backend/POS state.

This order keeps Central canonical authority available before an upgraded edge reconnects and keeps browser presentation last.

## Credential replacement

Follow Backend `docs/V1_DEPLOYMENT_SECRET_ROTATION.md`. Credential mismatch must fail closed while durable POS outbox/cursor state is retained. Do not clear queues/cursors or recreate business transactions during rotation.

## Failure policy

- **Central migration/config failure:** stop progression; forward-fix the deployment. Native PostgreSQL restore is reserved for verified destructive/corrupt state under the Database V1 same-tenant restore contract.
- **POS binary/startup failure:** stop the new binary and restore the previous reviewed binary/config. Do not replace/delete SQLite, device identity, local token or backups unless the database is proven damaged and the certified recovery procedure is invoked.
- **Frontend artifact failure:** roll back only the static/container artifact. Do not change Central/POS data to recover a presentation deployment.
- Never make manager-approval, transaction, pricing, inventory or refund policy changes as an operational rollback shortcut.

## Completion evidence

A production deployment is complete only when:

- Central `/health` and `/ready` are healthy;
- POS `/api/v1/health` and `/api/v1/ready` are healthy;
- POS outbox has no unexpected growing/dead-letter backlog and config/change-feed diagnostics are progressing;
- a post-deploy sale can be created offline/locally and converge exactly once to Central;
- the production Frontend serves the expected static artifact and SPA deep-link fallback;
- deployed POS version/source commit matches the retained release manifest.
