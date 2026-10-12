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
set -euo pipefail

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
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
ok()   { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}!${NC} $*"; }
fail() { echo -e "${RED}✗${NC} $*"; exit 1; }
step() { echo -e "\n${YELLOW}▶${NC} $*"; }

# ── Aufräumen beim Beenden ────────────────────────────────────────────────────
cleanup() {
  echo ""
  [[ -n "$BRIDGE_PID"  ]] && kill "$BRIDGE_PID"  2>/dev/null && warn "Bridge gestoppt"
  [[ -n "$GATEWAY_PID" ]] && kill "$GATEWAY_PID" 2>/dev/null && warn "Gateway gestoppt"
}
trap cleanup EXIT INT TERM

# ── 1. Voraussetzungen ────────────────────────────────────────────────────────
step "Voraussetzungen prüfen"
[[ -x "$SCRIPT_DIR/udal-gateway" ]]               || fail "udal-gateway nicht gefunden (chmod +x?)"
command -v python3 >/dev/null                      || fail "python3 nicht installiert"
[[ -f "$SCRIPT_DIR/beaglebone-bridge.py" ]]       || fail "beaglebone-bridge.py nicht gefunden"
[[ -f "$SCRIPT_DIR/beaglebone-bme280.schema.json" ]] || fail "beaglebone-bme280.schema.json nicht gefunden"
ok "Alle Dateien vorhanden"

# ── 2. BBB-Verbindung ─────────────────────────────────────────────────────────
step "BBB-Verbindung testen ($BBB_URL)"
curl -sf "$BBB_URL/api/v1/bme280" -o /dev/null \
  || fail "BBB nicht erreichbar — USB-Kabel prüfen oder BBB_URL setzen: BBB_URL=http://... $0"
ok "BBB antwortet"

# ── 3. Gateway starten ────────────────────────────────────────────────────────
step "UDAL Gateway starten (Port $GATEWAY_HTTP)"
UDAL_DEV_INSECURE=true \
UDAL_BOOTSTRAP_API_KEY="admin:admin:$API_KEY" \
UDAL_METRICS_ADDR=":$GATEWAY_METRICS" \
  "$SCRIPT_DIR/udal-gateway" &>/tmp/udal-gateway.log &
GATEWAY_PID=$!

# Warten bis Gateway ready
for i in $(seq 1 20); do
  sleep 0.5
  if curl -sf "http://localhost:$GATEWAY_METRICS/health" -o /dev/null 2>/dev/null; then
    ok "Gateway ready (PID $GATEWAY_PID)"
    break
  fi
  [[ $i -eq 20 ]] && { cat /tmp/udal-gateway.log; fail "Gateway nicht gestartet"; }
done

# ── 4. Bridge starten ─────────────────────────────────────────────────────────
step "Bridge starten (Port $BRIDGE_PORT → $BBB_URL)"
python3 "$SCRIPT_DIR/beaglebone-bridge.py" \
  --bbb-url "$BBB_URL" \
  --port "$BRIDGE_PORT" &>/tmp/udal-bridge.log &
BRIDGE_PID=$!

# Warten bis Bridge ready
for i in $(seq 1 10); do
  sleep 0.5
  if curl -sf "http://localhost:$BRIDGE_PORT/health" -o /dev/null 2>/dev/null; then
    ok "Bridge ready (PID $BRIDGE_PID)"
    break
  fi
  [[ $i -eq 10 ]] && { cat /tmp/udal-bridge.log; fail "Bridge nicht gestartet"; }
done

# ── 5. Capability-Schema registrieren ────────────────────────────────────────
step "Capability-Schema registrieren"
curl -sf -X POST "http://localhost:$GATEWAY_HTTP/api/v1/capabilities" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d @"$SCRIPT_DIR/beaglebone-bme280.schema.json" -o /dev/null
ok "Schema beaglebone-bme280@1.0.0 registriert"

# ── 6. Device registrieren ────────────────────────────────────────────────────
step "Device registrieren"
curl -sf -X POST "http://localhost:$GATEWAY_HTTP/api/v1/devices" \
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
  }" -o /dev/null
ok "Device $DEVICE registriert"

# ── 7. Erster Property-Read ───────────────────────────────────────────────────
step "Warte auf ersten Poll (5 s)..."
sleep 6

step "Properties lesen"
RESULT=$(curl -sf "http://localhost:$GATEWAY_HTTP/api/v1/devices/$DEVICE/properties" \
  -H "Authorization: Bearer $API_KEY")
echo "$RESULT" | python3 -c "
import json, sys
d = json.load(sys.stdin)
for k, v in d.items():
    val = list(v.values())[0]
    print(f'  {k}: {val}')
" 2>/dev/null || echo "  $RESULT"

# ── 8. Demo läuft ─────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  Demo läuft — Gateway: :$GATEWAY_HTTP  Bridge: :$BRIDGE_PORT${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "  Temperatur:  curl http://localhost:$GATEWAY_HTTP/api/v1/devices/$DEVICE/properties/temperature -H 'Authorization: Bearer $API_KEY'"
echo "  GPIO an:     curl -X POST http://192.168.7.2:5000/api/v1/gpio/60 -H 'Content-Type: application/json' -d '{\"value\":1}'"
echo "  GPIO aus:    curl -X POST http://192.168.7.2:5000/api/v1/gpio/60 -H 'Content-Type: application/json' -d '{\"value\":0}'"
echo ""
echo "  Logs: /tmp/udal-gateway.log  /tmp/udal-bridge.log"
echo "  Beenden: Ctrl+C"
echo ""

wait
