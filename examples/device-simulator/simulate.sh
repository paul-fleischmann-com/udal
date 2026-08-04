#!/bin/sh
# Simulates a temperature-sensor device (schema/examples/temperature-sensor.json)
# over MQTT, matching the gateway MQTT adapter's wire format
# (code/gateway/internal/adapters/mqtt/topics.go, value.go): topic
# udal/{deviceId}/props/{path}, payload a single-key JSON discriminated
# union ({"float":...} / {"int":...}).
#
# Two things run concurrently:
#   1. A periodic push loop (this file's main body) publishing fresh
#      temperature/humidity/battery_level readings and a heartbeat
#      (udal/{deviceId}/status — the gateway's presence monitor only
#      considers a device online if it's heard *something* on that topic
#      within its timeout; props/ publishes alone don't satisfy it).
#   2. respond(), backgrounded below: ReadProperty/WriteProperty over MQTT
#      are live request/response round trips, not just "last known value"
#      (see adapter.go's ReadProperty/WriteProperty) — the gateway
#      publishes to .../props/{path}/get and blocks waiting for a reply on
#      the bare .../props/{path} topic, or to .../props/{path}/set and
#      waits for an ack on .../set/ack. Without a device answering both,
#      every GetProperty/SetProperty call times out and eventually trips
#      the adapter's circuit breaker (5 consecutive failures -> 30s open),
#      exactly what happened before this existed.
set -eu

MQTT_HOST="${MQTT_HOST:-mosquitto}"
MQTT_PORT="${MQTT_PORT:-1883}"
DEVICE_ID="${DEVICE_ID:-sim-temp-1}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-5}"

# Last-known value per property, shared between the push loop and the
# get/set responder (which runs in a background pipeline, i.e. a separate
# subshell — a plain shell variable wouldn't be visible across that).
STATE_DIR=/tmp/state
mkdir -p "$STATE_DIR"

publish_prop() {
  mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "udal/${DEVICE_ID}/props/$1" -m "$2"
}

publish_heartbeat() {
  mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "udal/${DEVICE_ID}/status" -m "online"
}

# sample_interval_s/display_unit are writable config properties (not
# device-generated telemetry) — seed a default so GetProperty works even
# before any SetProperty call.
[ -f "$STATE_DIR/sample_interval_s" ] || echo '{"int":30}' > "$STATE_DIR/sample_interval_s"
[ -f "$STATE_DIR/display_unit" ] || echo '{"string":"celsius"}' > "$STATE_DIR/display_unit"

respond() {
  # -v (verbose) prints "topic payload" per received message — the exact
  # shape `read -r topic payload` expects (any spaces in payload land in
  # $payload too, since it's the last of exactly two read variables).
  mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
    -t "udal/${DEVICE_ID}/props/+/get" -t "udal/${DEVICE_ID}/props/+/set" -v |
  while read -r topic payload; do
    case "$topic" in
      *"/get")
        prop=${topic#udal/"${DEVICE_ID}"/props/}
        prop=${prop%/get}
        [ -f "$STATE_DIR/$prop" ] && publish_prop "$prop" "$(cat "$STATE_DIR/$prop")"
        ;;
      *"/set")
        prop=${topic#udal/"${DEVICE_ID}"/props/}
        prop=${prop%/set}
        echo "$payload" > "$STATE_DIR/$prop"
        mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "udal/${DEVICE_ID}/props/${prop}/set/ack" -m "ack"
        ;;
    esac
  done
}
respond &

echo "device-simulator: publishing as '${DEVICE_ID}' to ${MQTT_HOST}:${MQTT_PORT} every ${INTERVAL_SECONDS}s"

battery=100
while true; do
  # temperature: 18-24C, humidity: 30-60%, battery drains 1%/tick and wraps
  # (a "recharge" so a long-running demo doesn't sit at 0 forever).
  temp=$(awk 'BEGIN { srand(); printf "%.1f", 18 + rand() * 6 }')
  humidity=$(awk 'BEGIN { srand(); printf "%.1f", 30 + rand() * 30 }')
  battery=$(awk -v b="$battery" 'BEGIN { print (b > 0) ? b - 1 : 100 }')

  echo "{\"float\":${temp}}" > "$STATE_DIR/temperature"
  echo "{\"float\":${humidity}}" > "$STATE_DIR/humidity"
  echo "{\"int\":${battery}}" > "$STATE_DIR/battery_level"

  publish_prop temperature "{\"float\":${temp}}"
  publish_prop humidity "{\"float\":${humidity}}"
  publish_prop battery_level "{\"int\":${battery}}"
  publish_heartbeat

  sleep "$INTERVAL_SECONDS"
done
