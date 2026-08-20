# SHAJRetailProducts V1 Deployment / Configuration / Secrets Acceptance Matrix

Status: **RELEASE CERTIFIED**

## Authority boundary

- Central deployment owns canonical service configuration, tenant/platform secrets, PostgreSQL/external-service connectivity, browser-origin policy and public health/readiness boundaries.
- POSService is the installed offline edge runtime. Installation/upgrades preserve SQLite/device/token/config/backup state while Central endpoint/tenant/sync credentials are delivered only through explicit deployment configuration.
- Frontend is a build-time/static presentation artifact. It contains no Central credential and does not become business/configuration authority.
- Database Migration/Recovery, Security, Auth/Az, Observability and Central↔POS Sync remain the certified semantic authorities for migration, secret validation, identity, health and synchronization.

## Acceptance matrix

| Capability | Certified evidence | Status |
|---|---|---|
| Backend production container mode | Security V1 Backend #115: `NODE_ENV=production`, non-root Node runtime, lockfile-backed build | CERTIFIED |
| Backend startup fail-closed configuration | Security V1: separated JWT/platform keys, PostgreSQL TLS verification, production CORS/RabbitMQ/support/admin/warmup boundaries | CERTIFIED |
| Backend liveness/readiness deploy contract | Observability V1: dependency-independent `/health` and PostgreSQL-aware `/ready` | CERTIFIED |
| Frontend reproducible/static artifact | Frontend #81 excludes local env secrets from Docker build context; Frontend #82 builds the real nginx container and verifies exact SPA deep-link fallback | CERTIFIED |
| POS packaged runtime build | Release-domain gates plus POS #316 build `./cmd/posservice` and retain exact version/source provenance | CERTIFIED |
| POS Linux service boundary | POS #315 acceptance: dedicated user/group, explicit env file, restart policy, `NoNewPrivileges`, protected filesystem/home, restricted state paths and `UMask=0077` | CERTIFIED |
| POS Windows service boundary | POS #314 loads `pos.env` into SCM service environment; POS #315 preserves durable state; POS #317 requires complete production config before service creation/start | CERTIFIED |
| POS configuration examples | POS #317 production template matches runtime variable names, requires offline-grant public key, and keeps Central endpoint/tenant/token as one all-or-none authority set | CERTIFIED |
| POS production secret fail-closed policy | Security V1 POS #289 rejects missing/placeholder production credentials while retaining generated protected local-token mode | CERTIFIED |
| POS durable state across reinstall/upgrade | Database/Store-Device/Auth certifications plus POS #315 preserve SQLite, device identity, local token, config and backups independently of binaries | CERTIFIED |
| Central↔POS endpoint/credential atomicity | Sync V1 plus POS #317 require URL/tenant/token together and fail closed on tenant/device scope mismatch | CERTIFIED |
| Frontend runtime authority configuration | Frontend Completion + Security + #81: local-POS authority is explicit, network failure cannot switch business authority, and secret-bearing local env files are excluded from browser builds | CERTIFIED |
| Secret delivery/rotation boundary | Backend #133 certifies fail-closed POS sync/JWT/database credential replacement with durable outbox/cursor/state preservation and secret-free diagnostics | CERTIFIED |
| Version/artifact provenance | POS #316 embeds version + exact Git commit, emits `RELEASE-MANIFEST.txt`, and verifies `SHA256SUMS.txt` before installation | CERTIFIED |
| Upgrade/deploy ordering | `docs/V1_DEPLOYMENT_RUNBOOK.md` composes certified Database/Recovery + Sync behavior into Central → POS → Frontend ordering with forward-fix/restore boundaries | CERTIFIED |
| Deployment diagnostics | Observability V1 health/readiness, request correlation and bounded POS support diagnostics | CERTIFIED |
| Three-repository Deployment release gate | `.github/workflows/v1-deployment-release.yml` validates merged Backend deployment policy/container, POS packaging/provenance/full build, and merged Frontend context/build/nginx container | CERTIFIED |

## Release decision

**V1 DEPLOYMENT / CONFIGURATION / SECRETS RELEASE CERTIFIED.**

Production deployment now has fail-closed configuration and credential handling, non-root Central runtime, state-preserving POS installers, complete production POS configuration templates, traceable POS artifacts, secret-safe Frontend Docker context, real static-container/SPA acceptance, explicit credential replacement, deterministic Central → POS → Frontend deployment ordering, and a merged-main three-repository release gate.

Freeze this domain after merge except for defects. Secret values remain deployment/provider concerns and must never be committed or compiled into the Frontend.
