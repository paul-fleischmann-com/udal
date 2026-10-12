#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 Paul Fleischmann
#
# BeagleBone Black UDAL Demo — startet Gateway + Bridge, registriert
# Schema + Device und liest eine erste Property.
#
# Voraussetzungen im selben Verzeichnis:
#   ./udal-gateway
#   ./beaglebone-bridge.py
#   ./beaglebone-bme280.schema.json
set -uo pipefail

BBB_URL="${BBB_URL:-http://192.168.7.2:5000}"
BRIDGE_PORT="${BRIDGE_PORT:-5001}"
GATEWAY_HTTP="${GATEWAY_HTTP:-8080}"
GATEWAY_GRPC="${GATEWAY_GRPC:-50051}"
GATEWAY_METRICS="${GATEWAY_METRICS:-9091}"
API_KEY="demo-key-123"
DEVICE="beaglebone-01"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GATEWAY_PID=""
BRIDGE_PID=""

# ── Farben ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
ok()   { echo -e "${GREEN}✓${NC} $*"; }
info() { echo -e "${CYAN}  $*${NC}"; }
warn() { echo -e "${YELLOW}!${NC} $*"; }
fail() { echo -e "${RED}✗${NC} $*"; exit 1; }
step() { echo -e "\n${YELLOW}▶${NC} $*"; }

# ── Aufräumen beim Beenden ────────────────────────────────────────────────────
cleanup() {
  echo ""
  [[ -n "$BRIDGE_PID"  ]] && kill "$BRIDGE_PID"  2>/dev/null && warn "Bridge gestoppt (PID $BRIDGE_PID)"
  [[ -n "$GATEWAY_PID" ]] && kill "$GATEWAY_PID" 2>/dev/null && warn "Gateway gestoppt (PID $GATEWAY_PID)"
}
trap cleanup EXIT INT TERM

api() {
  local method="$1" path="$2" body="${3:-}"
  local url="http://localhost:$GATEWAY_HTTP$path"
  if [[ -n "$body" ]]; then
    curl -sf -X "$method" "$url" \
      -H "Authorization: Bearer $API_KEY" \
      -H "Content-Type: application/json" \
      -d "$body"
  else
    curl -sf "$url" -H "Authorization: Bearer $API_KEY"
  fi
}

# ── 1. Voraussetzungen ────────────────────────────────────────────────────────
step "Voraussetzungen prüfen"
[[ -x "$SCRIPT_DIR/udal-gateway" ]]                   || fail "udal-gateway nicht gefunden (chmod +x?)"
command -v python3 >/dev/null                          || fail "python3 nicht installiert"
[[ -f "$SCRIPT_DIR/beaglebone-bridge.py" ]]           || fail "beaglebone-bridge.py nicht gefunden"
[[ -f "$SCRIPT_DIR/beaglebone-bme280.schema.json" ]]  || fail "beaglebone-bme280.schema.json nicht gefunden"
ok "udal-gateway, python3, bridge, schema — alles vorhanden"

# ── 2. BBB-Verbindung ─────────────────────────────────────────────────────────
step "BBB-Verbindung testen ($BBB_URL)"
BBB_DATA=$(curl -sf "$BBB_URL/api/v1/bme280") \
  || fail "BBB nicht erreichbar — USB-Kabel prüfen oder: BBB_URL=http://... $0"
TEMP=$(echo "$BBB_DATA" | python3 -c "import json,sys; print(json.load(sys.stdin)['temperature'])" 2>/dev/null || echo "?")
ok "BBB antwortet — aktuelle Temperatur: ${TEMP}°C"

# ── 3. Gateway starten ────────────────────────────────────────────────────────
step "UDAL Gateway starten"
UDAL_DEV_INSECURE=true \
UDAL_BOOTSTRAP_API_KEY="admin:admin:$API_KEY" \
UDAL_METRICS_ADDR=":$GATEWAY_METRICS" \
  "$SCRIPT_DIR/udal-gateway" >>/tmp/udal-gateway.log 2>&1 &
GATEWAY_PID=$!

for i in $(seq 1 20); do
  sleep 0.5
  if curl -sf "http://localhost:$GATEWAY_METRICS/health" -o /dev/null 2>/dev/null; then
    ok "Gateway ready  — PID $GATEWAY_PID  REST :$GATEWAY_HTTP  gRPC :$GATEWAY_GRPC  metrics :$GATEWAY_METRICS"
    break
  fi
  if [[ $i -eq 20 ]]; then
    echo "--- Gateway-Log ---"
    tail -20 /tmp/udal-gateway.log
    fail "Gateway nicht gestartet nach 10 s"
  fi
done

# ── 4. Bridge starten ─────────────────────────────────────────────────────────
step "Bridge starten  :$BRIDGE_PORT → $BBB_URL"
python3 "$SCRIPT_DIR/beaglebone-bridge.py" \
  --bbb-url "$BBB_URL" \
  --port "$BRIDGE_PORT" >>/tmp/udal-bridge.log 2>&1 &
BRIDGE_PID=$!

for i in $(seq 1 10); do
  sleep 0.5
  if curl -sf "http://localhost:$BRIDGE_PORT/health" -o /dev/null 2>/dev/null; then
    SNAP=$(curl -sf "http://localhost:$BRIDGE_PORT/properties" \
      | python3 -c "import json,sys; d=json.load(sys.stdin); print(', '.join(f'{k}={list(v.values())[0]}' for k,v in d.items()))" 2>/dev/null || echo "")
    ok "Bridge ready    — PID $BRIDGE_PID  GET /properties → $SNAP"
    break
  fi
  if [[ $i -eq 10 ]]; then
    echo "--- Bridge-Log ---"
    tail -10 /tmp/udal-bridge.log
    fail "Bridge nicht gestartet nach 5 s"
  fi
done

# ── 5. Capability-Schema registrieren ────────────────────────────────────────
step "Capability-Schema registrieren"
RESP=$(curl -s -X POST "http://localhost:$GATEWAY_HTTP/api/v1/capabilities" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d @"$SCRIPT_DIR/beaglebone-bme280.schema.json")
if echo "$RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); exit(0 if 'name' in d or d.get('code')==6 else 1)" 2>/dev/null; then
  ok "Schema beaglebone-bme280@1.0.0 registriert"
else
  warn "Schema-Antwort: $RESP"
fi

# ── 6. Device registrieren ────────────────────────────────────────────────────
step "Device registrieren"
RESP=$(curl -s -X POST "http://localhost:$GATEWAY_HTTP/api/v1/devices" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\":       \"$DEVICE\",
    \"capability\": \"beaglebone-bme280@1.0.0\",
    \"transport\":  \"http\",
    \"labels\": {
      \"http.endpoint\":      \"http://localhost:$BRIDGE_PORT\",
      \"http.poll_interval\": \"5s\",
      \"hw\":                 \"beaglebone-black\"
    }
  }")
if echo "$RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); exit(0 if 'id' in d or d.get('code')==6 else 1)" 2>/dev/null; then
  DEVICE_ID=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id','(bereits vorhanden)'))" 2>/dev/null)
  ok "Device $DEVICE registriert  — ID: $DEVICE_ID"
else
  warn "Device-Antwort: $RESP"
fi

# ── 7. Erster Property-Read ───────────────────────────────────────────────────
step "Warte auf ersten HTTP-Adapter-Poll (5 s)..."
sleep 6

step "Properties lesen via UDAL Gateway"
PROPS=$(curl -s "http://localhost:$GATEWAY_HTTP/api/v1/devices/$DEVICE/properties" \
  -H "Authorization: Bearer $API_KEY")
echo "$PROPS" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    for k, v in d.items():
        val = list(v.values())[0]
        unit = {'temperature': '°C', 'humidity': '%rH', 'pressure': 'hPa'}.get(k, '')
        print(f'  {k:<14} {val} {unit}')
except Exception as e:
    print('  (parse error)', e)
    print(sys.stdin.read())
" 2>/dev/null || echo "  Rohdaten: $PROPS"

# ── 8. Demo läuft ─────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  Demo läuft${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
info "Gateway  http://localhost:$GATEWAY_HTTP   gRPC :$GATEWAY_GRPC   metrics :$GATEWAY_METRICS"
info "Bridge   http://localhost:$BRIDGE_PORT/properties"
info "Logs     /tmp/udal-gateway.log   /tmp/udal-bridge.log"
echo ""
echo -e "${CYAN}  Befehle:${NC}"
echo "  Alle Properties:  curl http://localhost:$GATEWAY_HTTP/api/v1/devices/$DEVICE/properties -H 'Authorization: Bearer $API_KEY'"
echo "  Temperatur:       curl http://localhost:$GATEWAY_HTTP/api/v1/devices/$DEVICE/properties/temperature -H 'Authorization: Bearer $API_KEY'"
echo "  GPIO USR0 an:     curl -X POST $BBB_URL/api/v1/gpio/60 -H 'Content-Type: application/json' -d '{\"value\":1}'"
echo "  GPIO USR0 aus:    curl -X POST $BBB_URL/api/v1/gpio/60 -H 'Content-Type: application/json' -d '{\"value\":0}'"
echo ""
echo "  Beenden: Ctrl+C"
echo ""

wait
