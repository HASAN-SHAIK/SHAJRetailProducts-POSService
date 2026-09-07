#!/usr/bin/env bash
set -euo pipefail

bin=${1:?usage: v1-persisted-token-permissions-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-persisted-token-permissions
rm -rf "$work"
mkdir -p "$work/state" "$work/backups" "$work/security"

token_file="$work/security/pos.token"
token='cycle-c-permissive-token-0123456789abcdef0123456789abcdef'
printf '%s\n' "$token" > "$token_file"
chmod 0644 "$token_file"
bytes_before=$(sha256sum "$token_file" | awk '{print $1}')
mode_before=$(stat -c '%a' "$token_file")

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4824
export POS_SQLITE_PATH="$work/state/pos.db"
unset POS_LOCAL_API_TOKEN || true
export POS_LOCAL_TOKEN_FILE="$token_file"
export POS_ALLOWED_ORIGINS=http://127.0.0.1:5173
export POS_CENTRAL_API_URL=
export POS_BACKUP_DIRECTORY="$work/backups"
export POS_BACKUP_INTERVAL=6h
export POS_BACKUP_RETENTION=2
export POS_OBSERVABILITY_INTERVAL=30s

set +e
"$bin" > "$work/pos.log" 2>&1 &
pid=$!
set -e

listener=false
for _ in $(seq 1 30); do
  if curl --silent --fail --max-time 1 http://127.0.0.1:4824/api/v1/health > "$work/health.json" 2>/dev/null; then listener=true; break; fi
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.5
done

login_status=000
login_body=''
if [[ "$listener" == true ]]; then
  login_status=$(curl --silent --show-error --max-time 5 --output "$work/login.json" --write-out '%{http_code}' \
    -H 'Content-Type: application/json' \
    -H "X-POS-Local-Token: $token" \
    --data-binary '{"user_id":"missing-cycle-c-permissions","pin":"2468"}' \
    http://127.0.0.1:4824/api/v1/auth/login || true)
  login_body=$(cat "$work/login.json" 2>/dev/null || true)
fi

process_alive=false
if kill -0 "$pid" 2>/dev/null; then process_alive=true; fi
if [[ "$process_alive" == true ]]; then kill "$pid" 2>/dev/null || true; fi
set +e
wait "$pid" 2>/dev/null
exit_code=$?
set -e

bytes_after=$(sha256sum "$token_file" | awk '{print $1}')
mode_after=$(stat -c '%a' "$token_file")
bytes_unchanged=false; [[ "$bytes_before" == "$bytes_after" ]] && bytes_unchanged=true
mode_unchanged=false; [[ "$mode_before" == "$mode_after" ]] && mode_unchanged=true
secret_leaked=false; grep -Fq "$token" "$work/pos.log" && secret_leaked=true || true
safe_error=false; grep -Eqi 'local API security|local token|permission|mode|owner' "$work/pos.log" && safe_error=true || true
sqlite_present=false; [[ -e "$work/state/pos.db" ]] && sqlite_present=true

echo "PERSISTED_TOKEN_MODE_BEFORE=$mode_before"
echo "PERSISTED_TOKEN_MODE_AFTER=$mode_after"
echo "PERSISTED_TOKEN_PERMISSIONS_LISTENER_REACHABLE=$listener"
echo "PERSISTED_TOKEN_PERMISSIONS_LOGIN_STATUS=$login_status"
echo "PERSISTED_TOKEN_PERMISSIONS_LOGIN_BODY=$login_body"
echo "PERSISTED_TOKEN_PERMISSIONS_PROCESS_ALIVE_BEFORE_STOP=$process_alive"
echo "PERSISTED_TOKEN_PERMISSIONS_EXIT_CODE=$exit_code"
echo "PERSISTED_TOKEN_PERMISSIONS_BYTES_UNCHANGED=$bytes_unchanged"
echo "PERSISTED_TOKEN_PERMISSIONS_MODE_UNCHANGED=$mode_unchanged"
echo "PERSISTED_TOKEN_PERMISSIONS_SAFE_ERROR_LOGGED=$safe_error"
echo "PERSISTED_TOKEN_PERMISSIONS_SECRET_LEAKED=$secret_leaked"
echo "PERSISTED_TOKEN_PERMISSIONS_SQLITE_PRESENT=$sqlite_present"
echo '--- POS log ---'
cat "$work/pos.log" || true

if [[ "$listener" == false && "$process_alive" == false && "$exit_code" -ne 0 && "$bytes_unchanged" == true && "$mode_unchanged" == true && "$safe_error" == true && "$secret_leaked" == false ]]; then
  echo 'PERSISTED_TOKEN_PERMISSIONS_RUNTIME_PASS=true'
  exit 0
fi

echo 'PERSISTED_TOKEN_PERMISSIONS_RUNTIME_PASS=false'
exit 1
