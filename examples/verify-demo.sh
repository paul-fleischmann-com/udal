#!/usr/bin/env bash
# Builds, starts, and verifies the UDAL docker-compose demonstrator stack
# (issue #30) end-to-end: register the simulated device, read/write a
# property, and confirm it reports online -- the same steps as
# examples/README.adoc / verify-demo.ps1 (the Windows equivalent of this
# script), using curl + jq. Used both by CI (see .github/workflows/ci.yml's
# "Demo -- Docker Compose build + verify" job) and for local Linux/Mac use.
#
# Env vars: API_KEY, DEVICE_ID, TIMEOUT_SECONDS, TEAR_DOWN=1 to `compose
# down` on exit (always, success or failure) instead of leaving it running.
set -euo pipefail

API_KEY="${API_KEY:-demo-api-key}"
DEVICE_ID="${DEVICE_ID:-sim-temp-1}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-60}"
TEAR_DOWN="${TEAR_DOWN:-0}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
COMPOSE_DIR="$REPO_ROOT/deployments/docker"

step() { echo "==> $*"; }
ok()   { echo "    OK: $*"; }
fail() { echo "    FAIL: $*" >&2; }

if command -v docker >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v podman >/dev/null 2>&1; then
  COMPOSE=(podman compose)
else
  fail "neither 'docker' nor 'podman' found on PATH"
  exit 1
fi

cleanup() {
  if [ "$TEAR_DOWN" = "1" ]; then
    step "Tearing down (TEAR_DOWN=1)..."
    (cd "$COMPOSE_DIR" && "${COMPOSE[@]}" down) || true
  fi
}

on_error() {
  fail "$1"
  echo
  echo "Recent gateway logs:"
  (cd "$COMPOSE_DIR" && "${COMPOSE[@]}" logs gateway --tail 50) || true
  echo
  echo "Recent device-simulator logs:"
  (cd "$COMPOSE_DIR" && "${COMPOSE[@]}" logs device-simulator --tail 20) || true
  cleanup
  exit 1
}

step "Using compose provider: ${COMPOSE[*]}"
cd "$COMPOSE_DIR"

step "Building and starting the stack..."
"${COMPOSE[@]}" up --build -d || on_error "compose up failed"
ok "containers started"

step "Waiting for the gateway to report ready (up to ${TIMEOUT_SECONDS}s)..."
health=""
deadline=$((SECONDS + TIMEOUT_SECONDS))
while [ "$SECONDS" -lt "$deadline" ]; do
  health="$(curl -sf http://localhost:9090/health || true)"
  if echo "$health" | jq -e '.status == "ok"' >/dev/null 2>&1; then
    break
  fi
  health=""
  sleep 1
done
[ -n "$health" ] || on_error "gateway did not become ready within ${TIMEOUT_SECONDS}s"
ok "gateway ready: $health"

step "Container states:"
"${COMPOSE[@]}" ps

step "Registering device '$DEVICE_ID'..."
register_body=$(jq -n --arg id "$DEVICE_ID" \
  '{id: $id, name: "Simulated Temperature Sensor", capability: "temperature-sensor", transport: "mqtt"}')
register_status=$(curl -s -o /tmp/verify-demo-register.json -w '%{http_code}' -X POST http://localhost:8080/v1/devices \
  -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" -d "$register_body")
if [ "$register_status" = "200" ]; then
  ok "registered: $(cat /tmp/verify-demo-register.json)"
elif [ "$register_status" = "409" ]; then
  ok "device already registered from a previous run (409) -- continuing"
else
  on_error "register failed with HTTP $register_status: $(cat /tmp/verify-demo-register.json)"
fi

step "Waiting for a live temperature reading (simulator publishes every few seconds)..."
prop=""
deadline=$((SECONDS + 30))
while [ "$SECONDS" -lt "$deadline" ]; do
  prop="$(curl -sf -H "X-API-Key: $API_KEY" "http://localhost:8080/v1/devices/$DEVICE_ID/properties/temperature" || true)"
  if echo "$prop" | jq -e '.value.floatVal != null' >/dev/null 2>&1; then
    break
  fi
  prop=""
  sleep 2
done
[ -n "$prop" ] || on_error "no temperature value became available within 30s"
ok "temperature = $(echo "$prop" | jq -r '.value.floatVal')"

step "Writing sample_interval_s = 10..."
write_resp="$(curl -sf -X PUT "http://localhost:8080/v1/devices/$DEVICE_ID/properties/sample_interval_s" \
  -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" -d '{"intVal": "10"}')" || on_error "write failed"
written=$(echo "$write_resp" | jq -r '.newValue.intVal')
[ "$written" = "10" ] || on_error "write did not round-trip: $write_resp"
ok "sample_interval_s = $written"

step "Checking device status..."
listed="$(curl -sf -H "X-API-Key: $API_KEY" "http://localhost:8080/v1/devices/$DEVICE_ID")" || on_error "GetDevice failed"
status=$(echo "$listed" | jq -r '.device.status')
if [ "$status" = "DEVICE_STATUS_ONLINE" ]; then
  ok "device status: $status"
else
  on_error "device status: '$status' (expected DEVICE_STATUS_ONLINE)"
fi

if command -v grpcurl >/dev/null 2>&1; then
  step "Subscribe smoke test via grpcurl (6s, expecting at least one event)..."
  output=$(timeout 6 grpcurl -plaintext -H "x-api-key: $API_KEY" \
    -d "{\"deviceId\": \"$DEVICE_ID\"}" localhost:50051 udal.v1.DeviceService/Subscribe || true)
  if [ -n "$output" ]; then
    ok "received a Subscribe event"
  else
    fail "no Subscribe event received in 6s"
  fi
else
  step "grpcurl not found on PATH -- skipping the Subscribe smoke test (optional; see examples/README.adoc step 5)"
fi

echo
echo "Demo verified successfully."
echo "  Gateway REST:   http://localhost:8080"
echo "  Gateway gRPC:   localhost:50051"
echo "  Health/metrics: http://localhost:9090/health"

cleanup
