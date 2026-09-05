#!/usr/bin/env bash
set -u

bin=${1:?usage: v1-login-empty-object-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-login-empty-object
rm -rf "$work"
mkdir -p "$work/state" "$work/backups"
local_token=cycle-c-login-empty-object-token-1234567890

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4809
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
  if curl --silent --fail --max-time 1 http://127.0.0.1:4809/api/v1/health > "$work/health-before.json" 2>/dev/null; then
    ready=true
    break
  fi
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.5
done

echo "EMPTY_OBJECT_LOGIN_INITIAL_HEALTH=$ready"
if [[ "$ready" != true ]]; then
  echo 'EMPTY_OBJECT_LOGIN_RUNTIME_PASS=false'
  cat "$work/pos.log" || true
  exit 2
fi

status=$(curl --silent --show-error --max-time 5 \
  --output "$work/response.json" \
  --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H "X-POS-Local-Token: $local_token" \
  --data-binary '{}' \
  http://127.0.0.1:4809/api/v1/auth/login || true)
body=$(cat "$work/response.json" 2>/dev/null || true)

process_alive=false
if kill -0 "$pid" 2>/dev/null; then process_alive=true; fi
after=$(curl --silent --output "$work/health-after.json" --write-out '%{http_code}' --max-time 2 http://127.0.0.1:4809/api/v1/health || true)

sqlite_ok=false
if [[ -s "$work/state/pos.db" ]]; then sqlite_ok=true; fi

echo "EMPTY_OBJECT_LOGIN_RESPONSE_STATUS=$status"
echo "EMPTY_OBJECT_LOGIN_RESPONSE_BODY=$body"
echo "EMPTY_OBJECT_LOGIN_PROCESS_ALIVE=$process_alive"
echo "EMPTY_OBJECT_LOGIN_HEALTH_AFTER=$after"
echo "EMPTY_OBJECT_LOGIN_SQLITE_PRESENT=$sqlite_ok"
echo '--- POS log ---'
cat "$work/pos.log"

if [[ "$status" == "400" && "$body" == *'invalid_auth_payload'* && "$process_alive" == "true" && "$after" == "200" && "$sqlite_ok" == "true" ]]; then
  echo 'EMPTY_OBJECT_LOGIN_RUNTIME_PASS=true'
  exit 0
fi

echo 'EMPTY_OBJECT_LOGIN_RUNTIME_PASS=false'
exit 1
