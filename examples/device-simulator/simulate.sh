#!/bin/sh
# Publishes fake temperature-sensor telemetry (schema/examples/temperature-sensor.json)
# over MQTT, matching the gateway MQTT adapter's wire format
# (code/gateway/internal/adapters/mqtt/topics.go, value.go): topic
# udal/{deviceId}/props/{path}, payload a single-key JSON discriminated
# union ({"float":...} / {"int":...}).
#
# Also publishes to udal/{deviceId}/status (the heartbeat topic, issue #42)
# every tick — the gateway's presence monitor (internal/heartbeat) only
# considers a device online if it's heard *something* on that topic within
# its timeout (default 90s; payload content is never inspected), which
# props/ publishes alone don't satisfy. Without this, the dashboard/
# ListDevices would show sim-temp-1 as offline despite live telemetry.
set -eu

MQTT_HOST="${MQTT_HOST:-mosquitto}"
MQTT_PORT="${MQTT_PORT:-1883}"
DEVICE_ID="${DEVICE_ID:-sim-temp-1}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-5}"

publish_prop() {
  mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "udal/${DEVICE_ID}/props/$1" -m "$2"
}

publish_heartbeat() {
  mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "udal/${DEVICE_ID}/status" -m "online"
}

echo "device-simulator: publishing as '${DEVICE_ID}' to ${MQTT_HOST}:${MQTT_PORT} every ${INTERVAL_SECONDS}s"

battery=100
while true; do
  # temperature: 18-24C, humidity: 30-60%, battery drains 1%/tick and wraps
  # (a "recharge" so a long-running demo doesn't sit at 0 forever).
  temp=$(awk 'BEGIN { srand(); printf "%.1f", 18 + rand() * 6 }')
  humidity=$(awk 'BEGIN { srand(); printf "%.1f", 30 + rand() * 30 }')
  battery=$(awk -v b="$battery" 'BEGIN { print (b > 0) ? b - 1 : 100 }')

  publish_prop temperature "{\"float\":${temp}}"
  publish_prop humidity "{\"float\":${humidity}}"
  publish_prop battery_level "{\"int\":${battery}}"
  publish_heartbeat

  sleep "$INTERVAL_SECONDS"
done
