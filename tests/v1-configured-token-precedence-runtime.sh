#!/usr/bin/env bash
set -euo pipefail

bin=${1:?usage: v1-configured-token-precedence-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-configured-token-precedence
rm -rf "$work"
mkdir -p "$work/state" "$work/security" "$work/backups"

configured=$(printf 'c%.0s' $(seq 1 64))
stale='short-stale-token'
printf '%s\n' "$stale" > "$work/security/pos.token"
chmod 600 "$work/security/pos.token"
before_hash=$(sha256sum "$work/security/pos.token" | awk '{print $1}')

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4822
export POS_SQLITE_PATH="$work/state/pos.db"
export POS_LOCAL_API_TOKEN="$configured"
export POS_LOCAL_TOKEN_FILE="$work/security/pos.token"
export POS_ALLOWED_ORIGINS=http://127.0.0.1:5173
export POS_CENTRAL_API_URL=
export POS_BACKUP_DIRECTORY="$work/backups"
export POS_BACKUP_INTERVAL=6h
export POS_BACKUP_RETENTION=2
export POS_OBSERVABILITY_INTERVAL=30s

"$bin" > "$work/pos.log" 2>&1 &
pid=$!
cleanup(){ kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; }
trap cleanup EXIT

ready=false
for _ in $(seq 1 40); do
  if curl --silent --fail --max-time 1 http://127.0.0.1:4822/api/v1/health > "$work/health-before.json" 2>/dev/null; then ready=true; break; fi
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.5
done

if [[ "$ready" != true ]]; then
  echo 'CONFIGURED_TOKEN_PRECEDENCE_INITIAL_HEALTH=false'
  echo 'CONFIGURED_TOKEN_PRECEDENCE_RUNTIME_PASS=false'
  cat "$work/pos.log" || true
  exit 2
fi

after_hash=$(sha256sum "$work/security/pos.token" | awk '{print $1}')
file_unchanged=false; [[ "$before_hash" == "$after_hash" ]] && file_unchanged=true

configured_response="$work/configured-response.json"
configured_status=$(curl --silent --show-error --max-time 5 --output "$configured_response" --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H "X-POS-Local-Token: $configured" \
  --data-binary '{"user_id":"missing-cycle-c-configured","pin":"2468"}' \
  http://127.0.0.1:4822/api/v1/auth/login || true)
configured_body=$(cat "$configured_response" 2>/dev/null || true)

stale_response="$work/stale-response.json"
stale_status=$(curl --silent --show-error --max-time 5 --output "$stale_response" --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H "X-POS-Local-Token: $stale" \
  --data-binary '{"user_id":"missing-cycle-c-configured","pin":"2468"}' \
  http://127.0.0.1:4822/api/v1/auth/login || true)
stale_body=$(cat "$stale_response" 2>/dev/null || true)

process_alive=false; kill -0 "$pid" 2>/dev/null && process_alive=true
health_after=$(curl --silent --output "$work/health-after.json" --write-out '%{http_code}' --max-time 2 http://127.0.0.1:4822/api/v1/health || true)
sqlite_ok=false; [[ -s "$work/state/pos.db" ]] && sqlite_ok=true
secret_leaked=false; grep -Fq "$configured" "$work/pos.log" && secret_leaked=true

printf 'CONFIGURED_TOKEN_PRECEDENCE_INITIAL_HEALTH=true\n'
printf 'CONFIGURED_TOKEN_PRECEDENCE_FILE_UNCHANGED=%s\n' "$file_unchanged"
printf 'CONFIGURED_TOKEN_PRECEDENCE_CONFIGURED_STATUS=%s\n' "$configured_status"
printf 'CONFIGURED_TOKEN_PRECEDENCE_CONFIGURED_BODY=%s\n' "$configured_body"
printf 'CONFIGURED_TOKEN_PRECEDENCE_STALE_STATUS=%s\n' "$stale_status"
printf 'CONFIGURED_TOKEN_PRECEDENCE_STALE_BODY=%s\n' "$stale_body"
printf 'CONFIGURED_TOKEN_PRECEDENCE_PROCESS_ALIVE=%s\n' "$process_alive"
printf 'CONFIGURED_TOKEN_PRECEDENCE_HEALTH_AFTER=%s\n' "$health_after"
printf 'CONFIGURED_TOKEN_PRECEDENCE_SQLITE_PRESENT=%s\n' "$sqlite_ok"
printf 'CONFIGURED_TOKEN_PRECEDENCE_SECRET_LEAKED=%s\n' "$secret_leaked"

if [[ "$file_unchanged" == true && "$configured_status" == 401 && "$configured_body" == *'invalid_local_credentials'* && "$stale_status" == 401 && "$stale_body" == *'local_auth_required'* && "$process_alive" == true && "$health_after" == 200 && "$sqlite_ok" == true && "$secret_leaked" == false ]]; then
  echo 'CONFIGURED_TOKEN_PRECEDENCE_RUNTIME_PASS=true'
  exit 0
fi

echo 'CONFIGURED_TOKEN_PRECEDENCE_RUNTIME_PASS=false'
echo '--- POS log ---'
cat "$work/pos.log" || true
exit 1
