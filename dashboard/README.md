# udal-dashboard

Reference web dashboard for [UDAL](https://github.com/paul-fleischmann-com/udal)
(Universal Device Abstraction Layer), built with [Reflex](https://reflex.dev) —
device list, property browser, command dispatch, and live telemetry, using the
[Python SDK](../code/sdk/python) internally (issue #18).

## Run locally

```sh
# from the repo root
cd code/sdk/python && pip install -e .
cd ../../../dashboard && pip install -e ".[dev]"

# point at a running gateway (defaults: localhost:50051, no API key)
export UDAL_GATEWAY_URL=localhost:50051
export UDAL_API_KEY=your-api-key   # if the gateway requires one

reflex run
```

Then open http://localhost:3000.

## What it demonstrates

- **Device list** (req42.adoc F-03/F-04): polls `Client.list_devices()` on an
  interval and shows each device's online/offline status. Polling, not a true
  push — the gateway's `ListDevices` RPC has no streaming variant, and
  `Subscribe` requires a specific device's ID, not "all devices".
- **Property browser** (F-05/F-06): read a named property via
  `Client.get_property`, write one via `Client.write_property`. There's no
  "list properties" operation on the gateway's API, so you need to already
  know a property's path (from the device's capability schema).
- **Command dispatch** (F-07): `Client.send_command` with JSON-encoded
  parameters.
- **Live telemetry** (F-08): `Client.subscribe` — a genuine server push, shown
  as a live-updating feed with no page reload.

## Demo against a real device

```sh
docker run -d -p 1883:1883 eclipse-mosquitto:2
# start the gateway with UDAL_MQTT_BROKER=tcp://localhost:1883, then
# register an mqtt-transport device and publish to it to see the dashboard
# update live.
```
