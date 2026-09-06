#!/usr/bin/env bash
set -euo pipefail

bin=${1:?usage: v1-generated-local-token-permissions-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-generated-token-permissions
rm -rf "$work"
mkdir -p "$work/state" "$work/backups"

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4820
export POS_SQLITE_PATH="$work/state/pos.db"
unset POS_LOCAL_API_TOKEN || true
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
  if curl --silent --fail --max-time 1 http://127.0.0.1:4820/api/v1/health > "$work/health-before.json" 2>/dev/null; then ready=true; break; fi
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.5
done

echo "GENERATED_TOKEN_INITIAL_HEALTH=$ready"
if [[ "$ready" != true ]]; then
  echo 'GENERATED_TOKEN_PERMISSIONS_RUNTIME_PASS=false'
  cat "$work/pos.log" || true
  exit 2
fi

if [[ ! -s "$POS_LOCAL_TOKEN_FILE" ]]; then
  echo 'GENERATED_TOKEN_FILE_PRESENT=false'
  echo 'GENERATED_TOKEN_PERMISSIONS_RUNTIME_PASS=false'
  exit 1
fi

token=$(tr -d '\r\n' < "$POS_LOCAL_TOKEN_FILE")
mode=$(stat -c '%a' "$POS_LOCAL_TOKEN_FILE")
size=$(wc -c < "$POS_LOCAL_TOKEN_FILE" | tr -d ' ')
response="$work/response.json"
status=$(curl --silent --show-error --max-time 5 --output "$response" --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H "X-POS-Local-Token: $token" \
  --data-binary '{"user_id":"missing-cycle-c-permissions","pin":"2468"}' \
  http://127.0.0.1:4820/api/v1/auth/login || true)
body=$(cat "$response" 2>/dev/null || true)
process_alive=false; if kill -0 "$pid" 2>/dev/null; then process_alive=true; fi
after=$(curl --silent --output "$work/health-after.json" --write-out '%{http_code}' --max-time 2 http://127.0.0.1:4820/api/v1/health || true)
sqlite_ok=false; if [[ -s "$work/state/pos.db" ]]; then sqlite_ok=true; fi
secret_leaked=false; if grep -Fq "$token" "$work/pos.log"; then secret_leaked=true; fi

echo 'GENERATED_TOKEN_FILE_PRESENT=true'
echo "GENERATED_TOKEN_FILE_MODE=$mode"
echo "GENERATED_TOKEN_FILE_SIZE=$size"
echo "GENERATED_TOKEN_LOGIN_STATUS=$status"
echo "GENERATED_TOKEN_LOGIN_BODY=$body"
echo "GENERATED_TOKEN_PROCESS_ALIVE=$process_alive"
echo "GENERATED_TOKEN_HEALTH_AFTER=$after"
echo "GENERATED_TOKEN_SQLITE_PRESENT=$sqlite_ok"
echo "GENERATED_TOKEN_SECRET_LEAKED=$secret_leaked"
echo '--- POS log ---'
cat "$work/pos.log"

if [[ "$mode" == "600" && "$size" -ge 33 && "$status" == "401" && "$body" == *'invalid_local_credentials'* && "$process_alive" == true && "$after" == "200" && "$sqlite_ok" == true && "$secret_leaked" == false ]]; then
  echo 'GENERATED_TOKEN_PERMISSIONS_RUNTIME_PASS=true'
  exit 0
fi

echo 'GENERATED_TOKEN_PERMISSIONS_RUNTIME_PASS=false'
exit 1
