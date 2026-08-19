# SHAJRetailProducts V1 Deployment / Configuration / Secrets Acceptance Matrix

Status: **IN PROGRESS**

## Authority boundary

- Central deployment owns canonical service configuration, tenant/platform secrets, PostgreSQL/RabbitMQ connectivity, browser-origin policy and public health/readiness boundaries.
- POSService is the installed offline edge runtime. Production installation must preserve the local SQLite/device/token/backup state while receiving Central endpoint/tenant/sync credentials only through explicit deployment configuration.
- Frontend is a build-time/static presentation artifact. It must not embed Central credentials or become a secret/configuration authority.
- Database migrations, backup/restore, Security, Auth/Az, Observability and Central↔POS configuration synchronization remain the already-certified authorities for their respective runtime semantics.

## Acceptance matrix

| Capability | Current evidence | Status | Release requirement |
|---|---|---|---|
| Backend production container mode | Security V1 Backend #115 runs Central with `NODE_ENV=production` as non-root `node`; production secret/TLS/CORS policies therefore activate in the shipped image | CERTIFIED | preserve non-root production startup and lockfile-based install/build |
| Backend startup fail-closed configuration | Security V1 certifies JWT/platform secrets, PostgreSQL TLS verification, RabbitMQ production configuration when enabled, support intake, admin bootstrap and warmup-key boundaries | CERTIFIED | required production configuration must fail closed without unsafe defaults |
| Backend liveness/readiness deploy contract | Observability V1 certifies dependency-independent `/health` and PostgreSQL-aware `/ready` | CERTIFIED | deployment health probes must use the correct boundary |
| Frontend reproducible production artifact | Frontend Completion/Security certify committed lockfile, production build and cookie/POS authority boundaries; Dockerfile builds with Node 24 and serves static build via nginx | PARTIAL | certify container/static-server behavior, SPA fallback and no secret-bearing build defaults |
| POS packaged runtime build | Multiple release domains build `./cmd/posservice`; packaged runtime contains certified SQLite/outbox/inbox/config/backup/diagnostics behavior | CERTIFIED | keep package/build gate green |
| POS Linux service installation boundary | `packaging/linux/shajretail-pos.service` runs as dedicated user, reads `/etc/shajretail-pos/pos.env`, uses restart-on-failure, `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, restricted write paths and `UMask=0077` | PARTIAL | add focused packaging acceptance and preserve explicit config/state paths |
| POS Windows service configuration | Installer creates `%ProgramData%\SHAJRetail\POSService\pos.env`, DB/token/backup paths and automatic restart service, but the service command currently does not consume that env file | GAP | production Windows service must load the exact generated deployment environment before config.Load() and restart safely |
| POS configuration examples | `.env.example` and `packaging/pos.env.example` use placeholders for Central sync authority and local-only defaults for paths/listener | PARTIAL | ensure examples contain no usable credentials and match packaged runtime variable names/production policy |
| POS production secret fail-closed policy | Security V1 POS #289 requires offline-grant public key, rejects placeholder sync/local secrets and preserves secure generated local token mode | CERTIFIED | installer/config delivery must not bypass runtime policy |
| POS durable state across reinstall/restart | Database/Store-Device/Auth certifications prove SQLite/device/token/config/backup state survives runtime restart; installer separates binary under Program Files/opt from data under ProgramData/var | PARTIAL | certify reinstall/upgrade does not delete/replace data/token/backup state |
| Central↔POS endpoint/credential atomicity | Sync V1 and POS config require Central endpoint, tenant and sync token as one authority set and fail closed on tenant/device scope mismatch | CERTIFIED | deployment must deliver a consistent endpoint/tenant/token set |
| Frontend runtime authority configuration | Frontend local-POS mode and Central cookie sessions are certified; browser business authority cannot be switched implicitly by network failure | PARTIAL | audit deployment environment/build defaults so local POS vs compatibility mode is explicit and no secret is compiled into browser JS |
| Secret delivery/rotation operational boundary | Security certifies fail-closed runtime behavior, but repository deployment artifacts do not yet provide a complete V1 operator rotation/replacement contract for Central secrets/POS sync credentials | PARTIAL | document/certify secret replacement without source control or durable-state loss; do not expose secret values in diagnostics |
| Version/artifact provenance | CI certifies exact Git heads and production builds but packaged installer/image version provenance is not yet a unified V1 release contract | PARTIAL | release artifacts must be traceable to the validated commit/version |
| Upgrade/deploy ordering | Database Quality V1 certifies POS and Central upgrades/migrations; Store/Device/Sync certify reconnect/replay | PARTIAL | establish runtime deployment ordering and rollback/forward-fix boundary around already-certified migration policy |
| Deployment diagnostics | Observability V1 certifies health/readiness and POS support diagnostics | CERTIFIED | installation/startup failures remain actionable and secret-safe |
| Three-repository Deployment release gate | Not yet established | GAP | merged Backend/POS/Frontend deployment/config acceptance + production builds/package acceptance must be green |

## Current audit priorities

1. Fix and certify Windows POS service environment loading without moving secrets into source or command-line arguments.
2. Certify Linux/Windows installer state-path, permissions/restart and upgrade-preservation behavior.
3. Audit Frontend static/container deployment defaults and browser-visible environment values.
4. Establish secret replacement/rotation and release artifact provenance/upgrade ordering using the already-certified Security/Database boundaries.
5. Add the merged-main three-repository Deployment release gate and record the final release decision.

## Release decision

**NOT YET RELEASE CERTIFIED.** Production runtime security, migrations, health/readiness and core package builds are already strongly covered. The first concrete deployment defect is Windows POS service environment wiring: the installer writes a production configuration file that the service process does not currently load. Deployment V1 remains blocked on that fix plus installer/state preservation, Frontend deployment defaults, secret replacement/provenance, and the final merged-main release gate.
