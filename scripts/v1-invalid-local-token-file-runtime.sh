#!/usr/bin/env bash
set -u
bin=${1:?usage: v1-invalid-local-token-file-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-invalid-local-token
rm -rf "$work"; mkdir -p "$work/state" "$work/backups" "$work/security"
printf 'short-cycle-c-token\n' > "$work/security/pos.token"
cp "$work/security/pos.token" "$work/security/pos.token.before"

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4820
export POS_SQLITE_PATH="$work/state/pos.db"
export POS_LOCAL_API_TOKEN=
export POS_LOCAL_TOKEN_FILE="$work/security/pos.token"
export POS_ALLOWED_ORIGINS=http://127.0.0.1:5173
export POS_CENTRAL_API_URL=
export POS_BACKUP_DIRECTORY="$work/backups"
export POS_BACKUP_INTERVAL=6h
export POS_BACKUP_RETENTION=2
export POS_OBSERVABILITY_INTERVAL=30s

set +e
"$bin" > "$work/pos.log" 2>&1 & pid=$!
for _ in $(seq 1 40); do
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 0.25
done
if kill -0 "$pid" 2>/dev/null; then
  echo 'INVALID_LOCAL_TOKEN_PROCESS_EXITED=false'
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rc=124
else
  wait "$pid"; rc=$?
  echo 'INVALID_LOCAL_TOKEN_PROCESS_EXITED=true'
fi
set -e

listener_status=$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 1 http://127.0.0.1:4820/api/v1/health || true)
token_unchanged=false; if cmp -s "$work/security/pos.token.before" "$work/security/pos.token"; then token_unchanged=true; fi
sqlite_present=false; if [[ -s "$work/state/pos.db" ]]; then sqlite_present=true; fi
error_logged=false; if grep -q 'initialize local API security' "$work/pos.log" && grep -q 'local API token file contains an invalid token' "$work/pos.log"; then error_logged=true; fi
secret_leaked=false; if grep -q 'short-cycle-c-token' "$work/pos.log"; then secret_leaked=true; fi

echo "INVALID_LOCAL_TOKEN_EXIT_CODE=$rc"
echo "INVALID_LOCAL_TOKEN_LISTENER_HTTP=${listener_status:-000}"
echo "INVALID_LOCAL_TOKEN_FILE_UNCHANGED=$token_unchanged"
echo "INVALID_LOCAL_TOKEN_SQLITE_PRESENT=$sqlite_present"
echo "INVALID_LOCAL_TOKEN_ERROR_LOGGED=$error_logged"
echo "INVALID_LOCAL_TOKEN_SECRET_LEAKED=$secret_leaked"
echo '--- POS log ---'; cat "$work/pos.log"

if [[ "$rc" -ne 0 && "${listener_status:-000}" == "000" && "$token_unchanged" == true && "$sqlite_present" == true && "$error_logged" == true && "$secret_leaked" == false ]]; then
  echo 'INVALID_LOCAL_TOKEN_RUNTIME_PASS=true'
  exit 0
fi
echo 'INVALID_LOCAL_TOKEN_RUNTIME_PASS=false'
exit 1
