#!/usr/bin/env bash
set -euo pipefail

bin=${1:?usage: v1-malformed-json-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-malformed-json
rm -rf "$work"
mkdir -p "$work/state" "$work/backups"
local_token=cycle-c-malformed-json-token-1234567890abcdef

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4803
export POS_SQLITE_PATH="$work/state/pos.db"
export POS_LOCAL_API_TOKEN="$local_token"
export POS_LOCAL_TOKEN_FILE="$work/pos.token"
export POS_ALLOWED_ORIGINS=http://127.0.0.1:5173
export POS_CENTRAL_API_URL=
export POS_BACKUP_DIRECTORY="$work/backups"
export POS_BACKUP_INTERVAL=6h
export POS_BACKUP_RETENTION=2
export POS_OBSERVABILITY_INTERVAL=30s

"$bin" > "$work/pos.log" 2>&1 &
pid=$!
cleanup() {
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}
trap cleanup EXIT

ready=false
for i in $(seq 1 40); do
  if curl --silent --fail --max-time 1 http://127.0.0.1:4803/api/v1/health > "$work/health-before.json" 2>/dev/null; then
    ready=true
    break
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
  sleep 0.5
done

echo "MALFORMED_JSON_INITIAL_HEALTH=$ready"
test "$ready" = true

status=$(curl --silent --show-error --max-time 5 \
  --output "$work/malformed-response.json" \
  --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H "X-POS-Local-Token: $local_token" \
  --data-binary '{"user_id":"cycle-c","pin":' \
  http://127.0.0.1:4803/api/v1/auth/login || true)

echo "MALFORMED_JSON_RESPONSE_STATUS=$status"
echo '--- malformed response ---'
cat "$work/malformed-response.json" || true
echo

test "$status" = "400"
kill -0 "$pid"

after=$(curl --silent --output "$work/health-after.json" --write-out '%{http_code}' --max-time 2 \
  http://127.0.0.1:4803/api/v1/health)
echo "MALFORMED_JSON_PROCESS_ALIVE=true"
echo "MALFORMED_JSON_HEALTH_AFTER=$after"
test "$after" = "200"

echo '--- POS log ---'
cat "$work/pos.log"
echo 'MALFORMED_JSON_RUNTIME_PASS=true'
