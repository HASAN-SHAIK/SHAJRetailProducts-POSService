#!/usr/bin/env bash
set -euo pipefail

bin=${1:?usage: v1-local-token-symlink-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-local-token-symlink
rm -rf "$work"
mkdir -p "$work/state" "$work/backups" "$work/security"

target="$work/external-secret"
token_file="$work/security/pos.token"
token='cycle-c-symlink-token-0123456789abcdef0123456789abcdef'
printf '%s\n' "$token" > "$target"
chmod 600 "$target"
ln -s "$target" "$token_file"
target_before=$(sha256sum "$target" | awk '{print $1}')

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4822
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
  if curl --silent --fail --max-time 1 http://127.0.0.1:4822/api/v1/health > "$work/health.json" 2>/dev/null; then listener=true; break; fi
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.5
done

login_status=000
login_body=''
if [[ "$listener" == true ]]; then
  login_status=$(curl --silent --show-error --max-time 5 --output "$work/login.json" --write-out '%{http_code}' \
    -H 'Content-Type: application/json' \
    -H "X-POS-Local-Token: $token" \
    --data-binary '{"user_id":"missing-cycle-c-symlink","pin":"2468"}' \
    http://127.0.0.1:4822/api/v1/auth/login || true)
  login_body=$(cat "$work/login.json" 2>/dev/null || true)
fi

process_alive=false
if kill -0 "$pid" 2>/dev/null; then process_alive=true; fi
if [[ "$process_alive" == true ]]; then kill "$pid" 2>/dev/null || true; fi
set +e
wait "$pid" 2>/dev/null
exit_code=$?
set -e

target_after=$(sha256sum "$target" | awk '{print $1}')
symlink_preserved=false; if [[ -L "$token_file" ]]; then symlink_preserved=true; fi
target_unchanged=false; if [[ "$target_before" == "$target_after" ]]; then target_unchanged=true; fi
secret_leaked=false; if grep -Fq "$token" "$work/pos.log"; then secret_leaked=true; fi
safe_error=false; if grep -Eqi 'local API security|local token|symlink|symbolic link' "$work/pos.log"; then safe_error=true; fi
sqlite_present=false; if [[ -e "$work/state/pos.db" ]]; then sqlite_present=true; fi

echo "LOCAL_TOKEN_SYMLINK_LISTENER_REACHABLE=$listener"
echo "LOCAL_TOKEN_SYMLINK_LOGIN_STATUS=$login_status"
echo "LOCAL_TOKEN_SYMLINK_LOGIN_BODY=$login_body"
echo "LOCAL_TOKEN_SYMLINK_PROCESS_ALIVE_BEFORE_STOP=$process_alive"
echo "LOCAL_TOKEN_SYMLINK_EXIT_CODE=$exit_code"
echo "LOCAL_TOKEN_SYMLINK_TARGET_UNCHANGED=$target_unchanged"
echo "LOCAL_TOKEN_SYMLINK_SYMLINK_PRESERVED=$symlink_preserved"
echo "LOCAL_TOKEN_SYMLINK_SAFE_ERROR_LOGGED=$safe_error"
echo "LOCAL_TOKEN_SYMLINK_SECRET_LEAKED=$secret_leaked"
echo "LOCAL_TOKEN_SYMLINK_SQLITE_PRESENT=$sqlite_present"
echo '--- POS log ---'
cat "$work/pos.log" || true

if [[ "$listener" == false && "$process_alive" == false && "$exit_code" -ne 0 && "$target_unchanged" == true && "$symlink_preserved" == true && "$safe_error" == true && "$secret_leaked" == false ]]; then
  echo 'LOCAL_TOKEN_SYMLINK_RUNTIME_PASS=true'
  exit 0
fi

echo 'LOCAL_TOKEN_SYMLINK_RUNTIME_PASS=false'
exit 1
