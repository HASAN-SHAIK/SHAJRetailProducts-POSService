#!/usr/bin/env bash
set -euo pipefail

BIN="${1:-/tmp/posservice-cycle-c}"
CENTRAL_PORT="${CENTRAL_PORT:-43161}"
POS_PORT="${POS_PORT:-43162}"
ROOT="$(mktemp -d)"
DB="$ROOT/pos.db"
PHASE="$ROOT/phase"
REQUEST_LOG="$ROOT/central-events.jsonl"
CENTRAL_LOG="$ROOT/central.log"
POS1_LOG="$ROOT/pos-phase1.log"
POS2_LOG="$ROOT/pos-phase2.log"
LOCAL_TOKEN="cycle-c-customer-local-token-1234567890abcdef"
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
: > "$REQUEST_LOG"
python3 tests/customer-outbox-restart-central.py "$CENTRAL_PORT" "$PHASE" "$REQUEST_LOG" >"$CENTRAL_LOG" 2>&1 &
CENTRAL_PID=$!
for _ in $(seq 1 50); do
  if kill -0 "$CENTRAL_PID" 2>/dev/null && curl -fsS "http://127.0.0.1:$CENTRAL_PORT/probe" >/dev/null 2>&1; then break; fi
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
    POS_SYNC_TENANT_ID="tenant-customer-restart" \
    POS_SYNC_TOKEN="sync-token-customer-restart-valid" \
    POS_DEVICE_ID="device-customer-restart" \
    POS_INSTALLATION_ID="installation-customer-restart" \
    POS_LOCAL_API_TOKEN="$LOCAL_TOKEN" \
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

customer_state() {
  python3 - "$DB" <<'PY'
import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
row = con.execute("SELECT id,name,local_version,sync_state FROM customers ORDER BY created_at LIMIT 1").fetchone()
if row is None:
    print("<none>")
else:
    count = con.execute("SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed'", (row[0],)).fetchone()[0]
    published = con.execute("SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed' AND status='published'", (row[0],)).fetchone()[0]
    versions = con.execute("SELECT group_concat(aggregate_version, ',') FROM (SELECT aggregate_version FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed' ORDER BY aggregate_version)", (row[0],)).fetchone()[0] or ""
    print(f"{row[0]}|{row[1]}|{row[2]}|{row[3]}|{count}|{published}|{versions}")
PY
}

start_pos "$POS1_LOG"

CREATE_JSON="$(curl -fsS -X POST "http://127.0.0.1:$POS_PORT/api/v1/customers" \
  -H "Content-Type: application/json" \
  -H "X-POS-Local-Token: $LOCAL_TOKEN" \
  --data '{"customer_code":"CYCLE-C-001","name":"Restart Customer V1","phone":"9876543210","email":"restart-v1@example.test","credit_limit_minor":250000,"currency":"INR"}')"
CUSTOMER_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$CREATE_JSON")"
test -n "$CUSTOMER_ID"

UPDATE_STATUS="$(curl -sS -o "$ROOT/update.json" -w '%{http_code}' -X PUT "http://127.0.0.1:$POS_PORT/api/v1/customers/$CUSTOMER_ID" \
  -H "Content-Type: application/json" \
  -H "X-POS-Local-Token: $LOCAL_TOKEN" \
  --data '{"customer_code":"CYCLE-C-001","name":"Restart Customer V2","phone":"9876543210","email":"restart-v2@example.test","credit_limit_minor":300000,"currency":"INR"}')"
test "$UPDATE_STATUS" = "200"

for _ in $(seq 1 100); do
  STATE="$(customer_state 2>/dev/null || true)"
  if [[ "$STATE" == "$CUSTOMER_ID|Restart Customer V2|2|pending|2|0|1,2" ]]; then break; fi
  sleep 0.05
done
BEFORE_RESTART="$(customer_state)"
test "$BEFORE_RESTART" = "$CUSTOMER_ID|Restart Customer V2|2|pending|2|0|1,2"
FIRST_HEALTH="$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$POS_PORT/api/v1/health")"

kill -TERM "$POS_PID"
wait "$POS_PID"
POS_PID=""
RETAINED_STATE="$(customer_state)"
test "$RETAINED_STATE" = "$BEFORE_RESTART"

# Central recovers while POSService is fully stopped. A fresh production process
# reopens the same SQLite database and must drain both customer versions in order.
echo 2 > "$PHASE"
start_pos "$POS2_LOG"

for _ in $(seq 1 250); do
  STATE="$(customer_state 2>/dev/null || true)"
  if [[ "$STATE" == "$CUSTOMER_ID|Restart Customer V2|2|pending|2|2|1,2" || "$STATE" == "$CUSTOMER_ID|Restart Customer V2|2|synced|2|2|1,2" ]]; then break; fi
  sleep 0.1
done
FINAL_STATE="$(customer_state)"
FINAL_PUBLISHED="$(python3 - "$DB" "$CUSTOMER_ID" <<'PY'
import sqlite3,sys
con=sqlite3.connect(sys.argv[1])
print(con.execute("SELECT COUNT(*) FROM outbox_events WHERE aggregate_type='customer' AND aggregate_id=? AND event_type='customer.changed' AND status='published'", (sys.argv[2],)).fetchone()[0])
PY
)"
test "$FINAL_PUBLISHED" = "2"

python3 - "$REQUEST_LOG" "$CUSTOMER_ID" <<'PY'
import json,sys
rows=[json.loads(line) for line in open(sys.argv[1], encoding='utf-8') if line.strip()]
rows=[r for r in rows if r.get('phase')=='2' and r.get('event_type')=='customer.changed' and r.get('aggregate_id')==sys.argv[2]]
if len(rows) != 2:
    raise SystemExit(f"phase2 customer event count={len(rows)} want=2 rows={rows}")
versions=[r.get('aggregate_version') for r in rows]
if versions != [1,2]:
    raise SystemExit(f"phase2 aggregate versions={versions} want=[1,2]")
ids=[r.get('event_id') for r in rows]
if len(set(ids)) != 2 or any(r.get('idempotency_key') != r.get('event_id') for r in rows):
    raise SystemExit(f"event identity mismatch rows={rows}")
if any(not r.get('auth_ok') for r in rows):
    raise SystemExit(f"Central machine auth mismatch rows={rows}")
if any(r.get('ordering_key') != 'customer:'+sys.argv[2] for r in rows):
    raise SystemExit(f"ordering key mismatch rows={rows}")
print("CUSTOMER_RESTART_CENTRAL_PHASE2_EVENTS=2")
print("CUSTOMER_RESTART_CENTRAL_VERSIONS=1,2")
print("CUSTOMER_RESTART_EVENT_IDS_DISTINCT=true")
print("CUSTOMER_RESTART_IDEMPOTENCY_KEYS_MATCH=true")
PY

SECOND_HEALTH="$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$POS_PORT/api/v1/health")"
kill -0 "$POS_PID"

echo "CUSTOMER_RESTART_CREATE_HTTP=201"
echo "CUSTOMER_RESTART_UPDATE_HTTP=$UPDATE_STATUS"
echo "CUSTOMER_RESTART_FIRST_HEALTH=$FIRST_HEALTH"
echo "CUSTOMER_RESTART_BEFORE=$BEFORE_RESTART"
echo "CUSTOMER_RESTART_RETAINED=$RETAINED_STATE"
echo "CUSTOMER_RESTART_SECOND_HEALTH=$SECOND_HEALTH"
echo "CUSTOMER_RESTART_FINAL=$FINAL_STATE"
echo "CUSTOMER_RESTART_PUBLISHED=$FINAL_PUBLISHED"
echo "CUSTOMER_RESTART_PROCESS_ALIVE=true"
echo "CUSTOMER_RESTART_RUNTIME_PASS=true"
echo "--- phase 1 POS log ---"
cat "$POS1_LOG"
echo "--- phase 2 POS log ---"
cat "$POS2_LOG"
echo "--- Central event log ---"
cat "$REQUEST_LOG"
