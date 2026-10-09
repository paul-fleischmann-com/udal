#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 Paul Fleischmann
#
# Thin HTTP bridge: adapts the BeagleBone Black REST API
# (GET /api/v1/bme280, GET /api/v1/gpio/{pin}) to the UDAL HTTP device
# convention expected by the gateway's HTTP adapter:
#
#   GET /properties          → {"temperature":{"float":…}, …}
#   GET /properties/{path}   → {"float":…} | {"int":…}
#
# Usage:
#   python3 beaglebone-bridge.py [--bbb-url http://192.168.7.2:5000] [--port 5001]
#
# Then register the device with:
#   labels["http.endpoint"] = "http://localhost:5001"

import argparse
import json
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

DEFAULT_BBB_URL = "http://192.168.7.2:5000"
DEFAULT_PORT    = 5001


def fetch_json(url: str) -> dict:
    with urllib.request.urlopen(url, timeout=5) as r:
        return json.loads(r.read())


def bbb_url(args) -> str:
    return args.bbb_url.rstrip("/")


def all_properties(bbb: str) -> dict:
    bme  = fetch_json(f"{bbb}/api/v1/bme280")
    gpio = fetch_json(f"{bbb}/api/v1/gpio/60")
    return {
        "temperature": {"float": float(bme["temperature"])},
        "humidity":    {"float": float(bme["humidity"])},
        "pressure":    {"float": float(bme["pressure"])},
        "gpio_usr0":   {"int":   int(gpio.get("value", 0))},
    }


def single_property(bbb: str, path: str) -> dict | None:
    if path in ("temperature", "humidity", "pressure"):
        bme = fetch_json(f"{bbb}/api/v1/bme280")
        return {"float": float(bme[path])}
    if path == "gpio_usr0":
        gpio = fetch_json(f"{bbb}/api/v1/gpio/60")
        return {"int": int(gpio.get("value", 0))}
    return None


class BridgeHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # keep stdout clean
        print(f"[bridge] {self.address_string()} {fmt % args}")

    def send_json(self, code: int, body: dict):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        bbb = self.server.bbb_url
        try:
            if self.path == "/properties":
                self.send_json(200, all_properties(bbb))
            elif self.path.startswith("/properties/"):
                prop = self.path.removeprefix("/properties/")
                val  = single_property(bbb, prop)
                if val is None:
                    self.send_json(404, {"error": f"unknown property: {prop}"})
                else:
                    self.send_json(200, val)
            elif self.path == "/health":
                self.send_json(200, {"status": "ok"})
            else:
                self.send_json(404, {"error": "not found"})
        except Exception as exc:
            self.send_json(502, {"error": str(exc)})


def main():
    parser = argparse.ArgumentParser(description="BeagleBone → UDAL HTTP bridge")
    parser.add_argument("--bbb-url", default=DEFAULT_BBB_URL)
    parser.add_argument("--port",    type=int, default=DEFAULT_PORT)
    args = parser.parse_args()

    server = HTTPServer(("", args.port), BridgeHandler)
    server.bbb_url = bbb_url(args)
    print(f"[bridge] listening on :{args.port}  →  {server.bbb_url}")
    server.serve_forever()


if __name__ == "__main__":
    main()
