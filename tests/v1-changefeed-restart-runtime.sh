#!/usr/bin/env bash
set -euo pipefail

BIN="${1:-/tmp/posservice-cycle-c}"
CENTRAL_PORT="${CENTRAL_PORT:-43151}"
POS_PORT="${POS_PORT:-43152}"
ROOT="$(mktemp -d)"
DB="$ROOT/pos.db"
PHASE="$ROOT/phase"
REQUEST_LOG="$ROOT/central-requests.log"
CENTRAL_LOG="$ROOT/central.log"
POS1_LOG="$ROOT/pos-phase1.log"
POS2_LOG="$ROOT/pos-phase2.log"
CENTRAL_PID=""
POS_PID=""

cleanup() {
  set +e
  if [[ -n "$POS_PID" ]] && kill -0 "$POS_PID" 2>/dev/null; then kill -TERM "$POS_PID"; wait "$POS_PID" 2>/dev/null; fi
  if [[ -n "$CENTRAL_PID" ]] && kill -0 "$CENTRAL_PID" 2>/dev/null; then kill -TERM "$CENTRAL_PID"; wait "$CENTRAL_PID" 2>/dev/null; fi
  rm -rf "$ROOT"
}
trap cleanup EXIT

echo 1 > "$PHASE"
python3 tests/changefeed-restart-central.py "$CENTRAL_PORT" "$PHASE" "$REQUEST_LOG" >"$CENTRAL_LOG" 2>&1 &
CENTRAL_PID=$!

for _ in $(seq 1 50); do
  if kill -0 "$CENTRAL_PID" 2>/dev/null && curl -sS "http://127.0.0.1:$CENTRAL_PORT/does-not-exist" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
kill -0 "$CENTRAL_PID"

start_pos() {
  local logfile="$1"
  env \
    POS_ENVIRONMENT=development \
    POS_LISTEN_ADDRESS="127.0.0.1:$POS_PORT" \
    POS_SQLITE_PATH="$DB" \
    POS_CENTRAL_API_URL="http://127.0.0.1:$CENTRAL_PORT" \
    POS_SYNC_TENANT_ID="tenant-cycle-c" \
    POS_SYNC_TOKEN="sync-token-cycle-c-valid" \
    POS_DEVICE_ID="device-cycle-c" \
    POS_INSTALLATION_ID="installation-cycle-c" \
    POS_LOCAL_API_TOKEN="cycle-c-local-api-token-1234567890-abcdef" \
    POS_LOCAL_TOKEN_FILE="$ROOT/local.token" \
    POS_SYNC_INTERVAL=200ms \
    POS_SYNC_REQUEST_TIMEOUT=1s \
    POS_BACKUP_INTERVAL=24h \
    POS_OBSERVABILITY_INTERVAL=24h \
    POS_BACKUP_DIRECTORY="$ROOT/backups" \
    "$BIN" >"$logfile" 2>&1 &
  POS_PID=$!

  for _ in $(seq 1 100); do
    if ! kill -0 "$POS_PID" 2>/dev/null; then
      echo "POS exited during startup" >&2
      cat "$logfile" >&2
      return 1
    fi
    if curl -fsS "http://127.0.0.1:$POS_PORT/api/v1/health" >/dev/null 2>&1; then return 0; fi
    sleep 0.1
  done
  echo "POS did not become healthy" >&2
  cat "$logfile" >&2
  return 1
}

state() {
  python3 - "$DB" <<'PY'
import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
def one(sql, default=""):
    row = con.execute(sql).fetchone()
    return default if row is None else row[0]
cursor = one("SELECT cursor_value FROM sync_checkpoints WHERE stream_name='central_changes'", "")
row = con.execute("SELECT name, version FROM catalog_products WHERE id='cycle-c-product'").fetchone()
name, version = ("", 0) if row is None else row
v1 = one("SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:cycle-c:v1' AND status='applied'", 0)
v2 = one("SELECT COUNT(*) FROM inbox_messages WHERE message_id='product:cycle-c:v2' AND status='applied'", 0)
print(f"{cursor}|{name}|{version}|{v1}|{v2}")
PY
}

wait_state() {
  local expected="$1"
  for _ in $(seq 1 100); do
    [[ -f "$DB" ]] || { sleep 0.1; continue; }
    local current
    current="$(state 2>/dev/null || true)"
    if [[ "$current" == "$expected" ]]; then return 0; fi
    sleep 0.1
  done
  echo "expected state: $expected" >&2
  echo "actual state:   $(state 2>/dev/null || true)" >&2
  return 1
}

start_pos "$POS1_LOG"
wait_state "cursor-1|Restart Milk V1|1|1|0"
FIRST_HEALTH="$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$POS_PORT/api/v1/health")"
FIRST_STATE="$(state)"

kill -TERM "$POS_PID"
wait "$POS_PID"
POS_PID=""
RETAINED_STATE="$(state)"

# Central advances while the POS is fully stopped. The same SQLite database is
# then reopened by a fresh production POSService process.
echo 2 > "$PHASE"
start_pos "$POS2_LOG"
wait_state "cursor-2|Restart Milk V2|2|1|1"
SECOND_HEALTH="$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$POS_PORT/api/v1/health")"
FINAL_STATE="$(state)"

if ! grep -q '^phase=2 changes cursor=cursor-1$' "$REQUEST_LOG"; then
  echo "fresh POS process did not resume its first phase-2 changefeed request from cursor-1" >&2
  cat "$REQUEST_LOG" >&2
  exit 1
fi
if grep -q '^phase=2 unexpected_cursor=' "$REQUEST_LOG"; then
  echo "Central observed an invalid restart cursor" >&2
  cat "$REQUEST_LOG" >&2
  exit 1
fi
kill -0 "$POS_PID"

echo "CHANGEFEED_RESTART_FIRST_HEALTH=$FIRST_HEALTH"
echo "CHANGEFEED_RESTART_FIRST_STATE=$FIRST_STATE"
echo "CHANGEFEED_RESTART_RETAINED_STATE=$RETAINED_STATE"
echo "CHANGEFEED_RESTART_PHASE2_RESUME_CURSOR=cursor-1"
echo "CHANGEFEED_RESTART_SECOND_HEALTH=$SECOND_HEALTH"
echo "CHANGEFEED_RESTART_FINAL_STATE=$FINAL_STATE"
echo "CHANGEFEED_RESTART_PROCESS_ALIVE=true"
echo "CHANGEFEED_RESTART_RUNTIME_PASS=true"
echo "--- phase 1 POS log ---"
cat "$POS1_LOG"
echo "--- phase 2 POS log ---"
cat "$POS2_LOG"
echo "--- Central request log ---"
cat "$REQUEST_LOG"
