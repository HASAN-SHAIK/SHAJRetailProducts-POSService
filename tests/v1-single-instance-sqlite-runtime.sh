#!/usr/bin/env bash
set -euo pipefail

binary=${1:?usage: v1-single-instance-sqlite-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-single-instance
state="$work/state"
runtime1="$work/runtime1"
runtime2="$work/runtime2"
rm -rf "$work"
mkdir -p "$state" "$runtime1/backups" "$runtime2/backups"

first_log="$work/first.log"
second_log="$work/second.log"
first_pid=
second_pid=
cleanup() {
  if [[ -n "${second_pid:-}" ]] && kill -0 "$second_pid" 2>/dev/null; then kill "$second_pid" 2>/dev/null || true; fi
  if [[ -n "${first_pid:-}" ]] && kill -0 "$first_pid" 2>/dev/null; then kill "$first_pid" 2>/dev/null || true; fi
  [[ -z "${second_pid:-}" ]] || wait "$second_pid" 2>/dev/null || true
  [[ -z "${first_pid:-}" ]] || wait "$first_pid" 2>/dev/null || true
}
trap cleanup EXIT

common_env=(
  POS_ENVIRONMENT=development
  POS_SQLITE_PATH="$state/pos.db"
  POS_LOCAL_API_TOKEN=cycle-c-single-instance-token-1234567890abcdef
  POS_ALLOWED_ORIGINS=http://127.0.0.1:5173
  POS_CENTRAL_API_URL=
  POS_BACKUP_INTERVAL=6h
  POS_BACKUP_RETENTION=2
  POS_OBSERVABILITY_INTERVAL=30s
)

env "${common_env[@]}" \
  POS_LISTEN_ADDRESS=127.0.0.1:4801 \
  POS_LOCAL_TOKEN_FILE="$runtime1/pos.token" \
  POS_BACKUP_DIRECTORY="$runtime1/backups" \
  "$binary" >"$first_log" 2>&1 &
first_pid=$!

first_ready=false
for _ in $(seq 1 40); do
  if curl --silent --fail --max-time 1 http://127.0.0.1:4801/api/v1/health >/dev/null 2>&1; then
    first_ready=true
    break
  fi
  kill -0 "$first_pid" 2>/dev/null || break
  sleep 0.5
done

echo "SINGLE_INSTANCE_FIRST_READY=$first_ready"
test "$first_ready" = true

# Start a second full POSService process against the exact same durable SQLite file.
env "${common_env[@]}" \
  POS_LISTEN_ADDRESS=127.0.0.1:4802 \
  POS_LOCAL_TOKEN_FILE="$runtime2/pos.token" \
  POS_BACKUP_DIRECTORY="$runtime2/backups" \
  "$binary" >"$second_log" 2>&1 &
second_pid=$!

second_ready=false
second_exited=false
for _ in $(seq 1 20); do
  if curl --silent --fail --max-time 1 http://127.0.0.1:4802/api/v1/health >/dev/null 2>&1; then
    second_ready=true
    break
  fi
  if ! kill -0 "$second_pid" 2>/dev/null; then
    second_exited=true
    break
  fi
  sleep 0.5
done

first_still_ready=false
if curl --silent --fail --max-time 1 http://127.0.0.1:4801/api/v1/health >/dev/null 2>&1; then
  first_still_ready=true
fi

echo "SINGLE_INSTANCE_SECOND_READY=$second_ready"
echo "SINGLE_INSTANCE_SECOND_EXITED=$second_exited"
echo "SINGLE_INSTANCE_FIRST_STILL_READY=$first_still_ready"
echo '--- first process log ---'
cat "$first_log"
echo '--- second process log ---'
cat "$second_log"

# Required V1 invariant: one durable POS SQLite store must be owned by only one runnable POSService process.
test "$first_still_ready" = true
test "$second_ready" = false
test "$second_exited" = true

echo 'SINGLE_INSTANCE_SQLITE_RUNTIME_PASS=true'
