#!/usr/bin/env bash
set -euo pipefail

bin=${1:?usage: v1-local-token-directory-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-local-token-directory
rm -rf "$work"
mkdir -p "$work/state" "$work/backups" "$work/security/pos.token"

token_path="$work/security/pos.token"

export POS_ENVIRONMENT=development
export POS_LISTEN_ADDRESS=127.0.0.1:4829
export POS_SQLITE_PATH="$work/state/pos.db"
unset POS_LOCAL_API_TOKEN || true
export POS_LOCAL_TOKEN_FILE="$token_path"
export POS_ALLOWED_ORIGINS=http://127.0.0.1:5173
export POS_CENTRAL_API_URL=
export POS_BACKUP_DIRECTORY="$work/backups"
export POS_BACKUP_INTERVAL=6h
export POS_BACKUP_RETENTION=2
export POS_OBSERVABILITY_INTERVAL=30s

"$bin" > "$work/pos.log" 2>&1 &
pid=$!

listener=false
exited=false
for _ in $(seq 1 12); do
  if curl --silent --fail --max-time 0.2 http://127.0.0.1:4829/api/v1/health >/dev/null 2>&1; then listener=true; break; fi
  if ! kill -0 "$pid" 2>/dev/null; then exited=true; break; fi
  sleep 0.25
done

process_alive=false
if kill -0 "$pid" 2>/dev/null; then process_alive=true; fi

exit_code=0
if [[ "$process_alive" == true ]]; then
  kill "$pid" 2>/dev/null || true
  set +e
  wait "$pid" 2>/dev/null
  exit_code=$?
  set -e
else
  set +e
  wait "$pid" 2>/dev/null
  exit_code=$?
  set -e
fi

directory_preserved=false
[[ -d "$token_path" ]] && directory_preserved=true
sqlite_present=false
[[ -e "$work/state/pos.db" ]] && sqlite_present=true
secret_leaked=false
if grep -q 'cycle-c-directory-secret' "$work/pos.log" 2>/dev/null; then secret_leaked=true; fi

runtime_pass=false
if [[ "$listener" == false && "$exited" == true && "$process_alive" == false && "$exit_code" -ne 0 && "$directory_preserved" == true && "$secret_leaked" == false ]]; then
  runtime_pass=true
fi

echo "LOCAL_TOKEN_DIRECTORY_LISTENER_REACHABLE=$listener"
echo "LOCAL_TOKEN_DIRECTORY_EXITED_WITHIN_BOUND=$exited"
echo "LOCAL_TOKEN_DIRECTORY_PROCESS_ALIVE_AFTER_BOUND=$process_alive"
echo "LOCAL_TOKEN_DIRECTORY_EXIT_CODE=$exit_code"
echo "LOCAL_TOKEN_DIRECTORY_PRESERVED=$directory_preserved"
echo "LOCAL_TOKEN_DIRECTORY_SQLITE_PRESENT=$sqlite_present"
echo "LOCAL_TOKEN_DIRECTORY_SECRET_LEAKED=$secret_leaked"
echo "LOCAL_TOKEN_DIRECTORY_RUNTIME_PASS=$runtime_pass"
echo '--- POS log ---'
cat "$work/pos.log" || true

[[ "$runtime_pass" == true ]]
