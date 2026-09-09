#!/usr/bin/env bash
set -u
bin=${1:?usage: v1-login-whitespace-pin-runtime.sh /path/to/posservice}
work=/tmp/shaj-cycle-c-login-whitespace-pin
rm -rf "$work"; mkdir -p "$work/state" "$work/backups"
local_token=cycle-c-login-whitespace-pin-token-1234567890
export POS_ENVIRONMENT=development POS_LISTEN_ADDRESS=127.0.0.1:4837 POS_SQLITE_PATH="$work/state/pos.db" POS_LOCAL_API_TOKEN="$local_token" POS_LOCAL_TOKEN_FILE="$work/pos.token" POS_ALLOWED_ORIGINS=http://127.0.0.1:5173 POS_CENTRAL_API_URL= POS_BACKUP_DIRECTORY="$work/backups" POS_BACKUP_INTERVAL=6h POS_BACKUP_RETENTION=2 POS_OBSERVABILITY_INTERVAL=30s
"$bin" > "$work/pos.log" 2>&1 & pid=$!
cleanup(){ kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; }; trap cleanup EXIT
ready=false
for i in $(seq 1 40); do if curl -sf --max-time 1 http://127.0.0.1:4837/api/v1/health > "$work/health-before.json" 2>/dev/null; then ready=true; break; fi; if ! kill -0 "$pid" 2>/dev/null; then break; fi; sleep 0.5; done
echo "WHITESPACE_PIN_LOGIN_INITIAL_HEALTH=$ready"
if [[ "$ready" != true ]]; then cat "$work/pos.log" || true; echo 'WHITESPACE_PIN_LOGIN_RUNTIME_PASS=false'; exit 2; fi
status=$(curl -sS --max-time 5 -o "$work/response.json" -w '%{http_code}' -H 'Content-Type: application/json' -H "X-POS-Local-Token: $local_token" --data-binary '{"user_id":"missing-cycle-c","pin":"   "}' http://127.0.0.1:4837/api/v1/auth/login || true)
body=$(cat "$work/response.json" 2>/dev/null || true)
process_alive=false; kill -0 "$pid" 2>/dev/null && process_alive=true
after=$(curl -s -o "$work/health-after.json" -w '%{http_code}' --max-time 2 http://127.0.0.1:4837/api/v1/health || true)
sqlite_ok=false; [[ -s "$work/state/pos.db" ]] && sqlite_ok=true
echo "WHITESPACE_PIN_LOGIN_RESPONSE_STATUS=$status"
echo "WHITESPACE_PIN_LOGIN_RESPONSE_BODY=$body"
echo "WHITESPACE_PIN_LOGIN_PROCESS_ALIVE=$process_alive"
echo "WHITESPACE_PIN_LOGIN_HEALTH_AFTER=$after"
echo "WHITESPACE_PIN_LOGIN_SQLITE_PRESENT=$sqlite_ok"
echo '--- POS log ---'; cat "$work/pos.log"
if [[ "$status" == "400" && "$body" == *'invalid_auth_payload'* && "$process_alive" == true && "$after" == 200 && "$sqlite_ok" == true ]]; then echo 'WHITESPACE_PIN_LOGIN_RUNTIME_PASS=true'; exit 0; fi
echo 'WHITESPACE_PIN_LOGIN_RUNTIME_PASS=false'; exit 1
