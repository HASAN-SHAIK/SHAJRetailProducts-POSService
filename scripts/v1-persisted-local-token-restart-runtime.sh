#!/usr/bin/env bash
set -u
bin=${1:?usage: v1-persisted-local-token-restart-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-persisted-local-token-restart
rm -rf "$work"; mkdir -p "$work/state" "$work/backups" "$work/security"

export POS_ENVIRONMENT=development
export POS_SQLITE_PATH="$work/state/pos.db"
export POS_LOCAL_API_TOKEN=
export POS_LOCAL_TOKEN_FILE="$work/security/pos.token"
export POS_ALLOWED_ORIGINS=http://127.0.0.1:5173
export POS_CENTRAL_API_URL=
export POS_BACKUP_DIRECTORY="$work/backups"
export POS_BACKUP_INTERVAL=6h
export POS_BACKUP_RETENTION=2
export POS_OBSERVABILITY_INTERVAL=30s

start_pos() {
  local port=$1 log=$2
  export POS_LISTEN_ADDRESS="127.0.0.1:$port"
  "$bin" > "$log" 2>&1 &
  POS_PID=$!
  for _ in $(seq 1 40); do
    if curl --silent --fail --max-time 1 "http://127.0.0.1:$port/api/v1/health" >/dev/null 2>&1; then return 0; fi
    if ! kill -0 "$POS_PID" 2>/dev/null; then return 1; fi
    sleep 0.5
  done
  return 1
}

stop_pos() {
  local pid=$1
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}

if ! start_pos 4821 "$work/pos-first.log"; then
  echo 'PERSISTED_LOCAL_TOKEN_FIRST_START=false'; cat "$work/pos-first.log" || true; exit 2
fi
first_pid=$POS_PID
echo 'PERSISTED_LOCAL_TOKEN_FIRST_START=true'
if [[ ! -s "$work/security/pos.token" ]]; then
  echo 'PERSISTED_LOCAL_TOKEN_CREATED=false'; stop_pos "$first_pid"; exit 3
fi
echo 'PERSISTED_LOCAL_TOKEN_CREATED=true'
cp "$work/security/pos.token" "$work/security/pos.token.before"
token=$(tr -d '\r\n' < "$work/security/pos.token")
token_len=${#token}
first_hash=$(sha256sum "$work/security/pos.token" | awk '{print $1}')
stop_pos "$first_pid"

if ! start_pos 4822 "$work/pos-second.log"; then
  echo 'PERSISTED_LOCAL_TOKEN_SECOND_START=false'; cat "$work/pos-second.log" || true; exit 4
fi
second_pid=$POS_PID
trap 'stop_pos "$second_pid"' EXIT
echo 'PERSISTED_LOCAL_TOKEN_SECOND_START=true'

status=$(curl --silent --show-error --max-time 5 --output "$work/login-response.json" --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  -H "X-POS-Local-Token: $token" \
  --data-binary '{"user_id":"missing-cycle-c","pin":"2468"}' \
  http://127.0.0.1:4822/api/v1/auth/login || true)
body=$(cat "$work/login-response.json" 2>/dev/null || true)
second_hash=$(sha256sum "$work/security/pos.token" | awk '{print $1}')
file_unchanged=false; if cmp -s "$work/security/pos.token.before" "$work/security/pos.token"; then file_unchanged=true; fi
process_alive=false; if kill -0 "$second_pid" 2>/dev/null; then process_alive=true; fi
health_after=$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 2 http://127.0.0.1:4822/api/v1/health || true)
sqlite_present=false; if [[ -s "$work/state/pos.db" ]]; then sqlite_present=true; fi

printf 'PERSISTED_LOCAL_TOKEN_LENGTH=%s\n' "$token_len"
printf 'PERSISTED_LOCAL_TOKEN_LOGIN_STATUS=%s\n' "$status"
printf 'PERSISTED_LOCAL_TOKEN_LOGIN_BODY=%s\n' "$body"
printf 'PERSISTED_LOCAL_TOKEN_FILE_UNCHANGED=%s\n' "$file_unchanged"
printf 'PERSISTED_LOCAL_TOKEN_FIRST_HASH=%s\n' "$first_hash"
printf 'PERSISTED_LOCAL_TOKEN_SECOND_HASH=%s\n' "$second_hash"
printf 'PERSISTED_LOCAL_TOKEN_PROCESS_ALIVE=%s\n' "$process_alive"
printf 'PERSISTED_LOCAL_TOKEN_HEALTH_AFTER=%s\n' "$health_after"
printf 'PERSISTED_LOCAL_TOKEN_SQLITE_PRESENT=%s\n' "$sqlite_present"
echo '--- first POS log ---'; cat "$work/pos-first.log"
echo '--- second POS log ---'; cat "$work/pos-second.log"

if [[ "$token_len" -ge 32 && "$status" == "401" && "$body" == *'invalid_local_credentials'* && "$file_unchanged" == true && "$first_hash" == "$second_hash" && "$process_alive" == true && "$health_after" == "200" && "$sqlite_present" == true ]]; then
  echo 'PERSISTED_LOCAL_TOKEN_RESTART_RUNTIME_PASS=true'
  exit 0
fi
echo 'PERSISTED_LOCAL_TOKEN_RESTART_RUNTIME_PASS=false'
exit 1
