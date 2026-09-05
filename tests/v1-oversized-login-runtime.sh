#!/usr/bin/env bash
set -euo pipefail

bin=${1:?usage: v1-oversized-login-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-oversized-login
rm -rf "$work"
mkdir -p "$work/state" "$work/backups"
local_token=cycle-c-oversized-login-token-1234567890abcdef

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4805
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
  if curl --silent --fail --max-time 1 http://127.0.0.1:4805/api/v1/health > "$work/health-before.json" 2>/dev/null; then
    ready=true
    break
  fi
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.5
done

echo "OVERSIZED_LOGIN_INITIAL_HEALTH=$ready"
test "$ready" = true

python3 - <<'PY' > "$work/request.json"
import json
print(json.dumps({"user_id": "x" * (33 * 1024), "pin": "2468"}))
PY

bytes=$(wc -c < "$work/request.json" | tr -d ' ')
status=$(curl --silent --show-error --max-time 5 \
  --output "$work/response.json" \
  --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H "X-POS-Local-Token: $local_token" \
  --data-binary @"$work/request.json" \
  http://127.0.0.1:4805/api/v1/auth/login || true)
body=$(cat "$work/response.json" 2>/dev/null || true)

process_alive=false
if kill -0 "$pid" 2>/dev/null; then process_alive=true; fi
after=$(curl --silent --output "$work/health-after.json" --write-out '%{http_code}' --max-time 2 http://127.0.0.1:4805/api/v1/health || true)

echo "OVERSIZED_LOGIN_REQUEST_BYTES=$bytes"
echo "OVERSIZED_LOGIN_RESPONSE_STATUS=$status"
echo "OVERSIZED_LOGIN_RESPONSE_BODY=$body"
echo "OVERSIZED_LOGIN_PROCESS_ALIVE=$process_alive"
echo "OVERSIZED_LOGIN_HEALTH_AFTER=$after"
echo '--- POS log ---'
cat "$work/pos.log"

if [[ "$bytes" -gt 32768 && "$status" == "400" && "$body" == *'invalid_auth_payload'* && "$process_alive" == "true" && "$after" == "200" ]]; then
  echo 'OVERSIZED_LOGIN_RUNTIME_PASS=true'
  exit 0
fi

echo 'OVERSIZED_LOGIN_RUNTIME_PASS=false'
exit 1
