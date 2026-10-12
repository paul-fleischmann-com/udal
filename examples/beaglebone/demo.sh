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

gw_get()  { curl -sf  "http://localhost:$GATEWAY_HTTP$1" -H "X-API-Key: $API_KEY"; }
gw_post() { curl -sf -X POST "http://localhost:$GATEWAY_HTTP$1" -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" -d "$2"; }

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
TEMP=$(echo "$BBB_DATA" | python3 -c "import json,sys; print(round(json.load(sys.stdin)['temperature'],2))" 2>/dev/null || echo "?")
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
    echo "--- Gateway-Log ---"; tail -20 /tmp/udal-gateway.log
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
    echo "--- Bridge-Log ---"; tail -10 /tmp/udal-bridge.log
    fail "Bridge nicht gestartet nach 5 s"
  fi
done

# ── 5. Capability-Schema registrieren ────────────────────────────────────────
step "Capability-Schema registrieren  (POST /v1/schemas)"
# bytes-Feld erwartet base64-codierten JSON-String
SCHEMA_B64=$(base64 -w0 < "$SCRIPT_DIR/beaglebone-bme280.schema.json")
RESP=$(curl -s -X POST "http://localhost:$GATEWAY_HTTP/v1/schemas" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"schema\":\"$SCHEMA_B64\"}")
NAME=$(echo "$RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('schema',{}).get('name','?')+'@'+d.get('schema',{}).get('version','?'))" 2>/dev/null || echo "")
if [[ -n "$NAME" && "$NAME" != "?@?" ]]; then
  ok "Schema $NAME registriert"
else
  warn "Schema-Antwort: $RESP"
fi

# ── 6. Device registrieren ────────────────────────────────────────────────────
step "Device registrieren  (POST /v1/devices)"
RESP=$(curl -s -X POST "http://localhost:$GATEWAY_HTTP/v1/devices" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"id\":         \"$DEVICE\",
    \"name\":       \"$DEVICE\",
    \"capability\": \"beaglebone-bme280@1.0.0\",
    \"transport\":  \"http\",
    \"labels\": {
      \"http.endpoint\":      \"http://localhost:$BRIDGE_PORT\",
      \"http.poll_interval\": \"5s\",
      \"hw\":                 \"beaglebone-black\"
    }
  }")
DEV_ID=$(echo "$RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('device',{}).get('id') or d.get('id',''))" 2>/dev/null || echo "")
if [[ -n "$DEV_ID" ]]; then
  ok "Device registriert  — ID: $DEV_ID"
else
  warn "Device-Antwort: $RESP"
fi

# ── 7. Erster Property-Read ───────────────────────────────────────────────────
step "Warte auf ersten HTTP-Adapter-Poll (5 s)..."
sleep 6

step "Properties lesen  (GET /v1/devices/$DEVICE/properties/{path})"
for PROP in temperature humidity pressure gpio_usr0; do
  RAW=$(curl -s "http://localhost:$GATEWAY_HTTP/v1/devices/$DEVICE/properties/$PROP" \
    -H "X-API-Key: $API_KEY")
  VAL=$(echo "$RAW" | python3 -c "
import json, sys
d = json.load(sys.stdin)
v = d.get('value', {})
print(list(v.values())[0] if v else '(leer)')
" 2>/dev/null || echo "$RAW")
  UNIT=$(python3 -c "print({'temperature':'°C','humidity':'%rH','pressure':'hPa'}.get('$PROP',''))" 2>/dev/null)
  printf "  %-14s %s %s\n" "$PROP" "$VAL" "$UNIT"
done

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
echo "  Temperatur:    curl http://localhost:$GATEWAY_HTTP/v1/devices/$DEVICE/properties/temperature -H 'X-API-Key: $API_KEY'"
echo "  Alle Devices:  curl http://localhost:$GATEWAY_HTTP/v1/devices -H 'X-API-Key: $API_KEY'"
echo "  GPIO USR0 an:  curl -X POST $BBB_URL/api/v1/gpio/60 -H 'Content-Type: application/json' -d '{\"value\":1}'"
echo "  GPIO USR0 aus: curl -X POST $BBB_URL/api/v1/gpio/60 -H 'Content-Type: application/json' -d '{\"value\":0}'"
echo ""
echo "  Beenden: Ctrl+C"
echo ""

wait
