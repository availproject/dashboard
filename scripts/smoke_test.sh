#!/usr/bin/env bash
# smoke_test.sh — end-to-end smoke test for dashboard-server.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-changeme}"
CONFIG="${CONFIG:-./config.yaml}"

PASS=0
FAIL=0
SERVER_PID=""

cleanup() {
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# ---- helpers ----

check() {
  local label="$1"
  local expected_code="$2"
  local actual_code="$3"
  local body="$4"
  if [ "$actual_code" = "$expected_code" ]; then
    echo "  [PASS] $label (HTTP $actual_code)"
    PASS=$((PASS + 1))
  else
    echo "  [FAIL] $label — expected HTTP $expected_code, got $actual_code"
    echo "         body: $body"
    FAIL=$((FAIL + 1))
  fi
}

http_get() {
  local path="$1"
  curl -s -o /tmp/smoke_body.txt -w "%{http_code}" \
    -H "Authorization: Bearer $TOKEN" \
    "$BASE_URL$path"
}

http_post() {
  local path="$1"
  local data="$2"
  curl -s -o /tmp/smoke_body.txt -w "%{http_code}" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "$data" \
    "$BASE_URL$path"
}

# ---- start server ----

echo "Starting dashboard-server..."
./dashboard-server --config "$CONFIG" > /tmp/smoke_server.log 2>&1 &
SERVER_PID=$!

# Wait for server to be ready (up to 10 seconds).
for i in $(seq 1 10); do
  if curl -s -o /dev/null "$BASE_URL/auth/login" 2>/dev/null; then
    break
  fi
  if [ "$i" = "10" ]; then
    echo "FAIL: server did not start within 10 seconds"
    cat /tmp/smoke_server.log
    exit 1
  fi
  sleep 1
done
echo "Server started (PID $SERVER_PID)"

# ---- login ----

echo ""
echo "Logging in..."
LOGIN_BODY=$(curl -s -X POST "$BASE_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
TOKEN=$(echo "$LOGIN_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null || echo "")

if [ -z "$TOKEN" ]; then
  echo "FAIL: login failed — no token in response"
  echo "Response: $LOGIN_BODY"
  exit 1
fi
echo "  [PASS] Login — got token"
PASS=$((PASS + 1))

# ---- GET /teams ----

echo ""
echo "GET /teams..."
CODE=$(http_get "/teams")
BODY=$(cat /tmp/smoke_body.txt)
check "GET /teams" "200" "$CODE" "$BODY"

# ---- GET /org/overview ----

echo ""
echo "GET /org/overview..."
CODE=$(http_get "/org/overview")
BODY=$(cat /tmp/smoke_body.txt)
check "GET /org/overview" "200" "$CODE" "$BODY"

# ---- POST /sync (org) ----

echo ""
echo "POST /sync scope=org..."
CODE=$(http_post "/sync" '{"scope":"org"}')
BODY=$(cat /tmp/smoke_body.txt)
check "POST /sync" "200" "$CODE" "$BODY"

RUN_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin).get('sync_run_id',''))" 2>/dev/null || echo "")
if [ -z "$RUN_ID" ] || [ "$RUN_ID" = "0" ]; then
  echo "  [FAIL] POST /sync — no sync_run_id in response"
  FAIL=$((FAIL + 1))
else
  echo "  sync_run_id=$RUN_ID"

  # ---- Poll until done (60-second timeout) ----
  echo ""
  echo "Polling GET /sync/$RUN_ID..."
  TIMEOUT=60
  STATUS=""
  for i in $(seq 1 $((TIMEOUT / 2))); do
    CODE=$(http_get "/sync/$RUN_ID")
    BODY=$(cat /tmp/smoke_body.txt)
    STATUS=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin).get('Status',''))" 2>/dev/null || echo "")
    if [ "$STATUS" = "done" ] || [ "$STATUS" = "error" ]; then
      break
    fi
    echo "  ... status=$STATUS (poll $i)"
    sleep 2
  done
  if [ "$STATUS" = "done" ]; then
    echo "  [PASS] Sync completed (status=done)"
    PASS=$((PASS + 1))
  elif [ "$STATUS" = "error" ]; then
    echo "  [FAIL] Sync ended with error"
    echo "         body: $BODY"
    FAIL=$((FAIL + 1))
  else
    echo "  [FAIL] Sync timed out after ${TIMEOUT}s (status=$STATUS)"
    FAIL=$((FAIL + 1))
  fi
fi

# ---- GET /teams/{id}/sprint ----

echo ""
echo "GET /teams for sprint test..."
CODE=$(http_get "/teams")
BODY=$(cat /tmp/smoke_body.txt)
TEAM_ID=$(echo "$BODY" | python3 -c "import sys,json; teams=json.load(sys.stdin); print(teams[0]['id'] if teams else '')" 2>/dev/null || echo "")

if [ -n "$TEAM_ID" ]; then
  echo "GET /teams/$TEAM_ID/sprint..."
  CODE=$(http_get "/teams/$TEAM_ID/sprint")
  BODY=$(cat /tmp/smoke_body.txt)
  check "GET /teams/$TEAM_ID/sprint" "200" "$CODE" "$BODY"
else
  echo "  [SKIP] No teams configured — skipping sprint test"
fi

# ---- summary ----

echo ""
echo "==============================="
if [ "$FAIL" -eq 0 ]; then
  echo "PASS — $PASS checks passed"
else
  echo "FAIL — $PASS passed, $FAIL failed"
  exit 1
fi
