# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 Paul Fleischmann
#
# BeagleBone Black UDAL Demo (Windows) — startet Gateway + Bridge,
# registriert Schema + Device und liest eine erste Property.
#
# Voraussetzungen im selben Verzeichnis:
#   .\udal-gateway.exe
#   .\beaglebone-bridge.py
#   .\beaglebone-bme280.schema.json
#
# Python 3 muss installiert sein: https://www.python.org/downloads/
#
# Starten:
#   .\demo.ps1
# Andere BBB-Adresse:
#   $env:BBB_URL="http://192.168.2.55:5000"; .\demo.ps1

param()

$BBB_URL        = if ($env:BBB_URL)         { $env:BBB_URL }         else { "http://192.168.7.2:5000" }
$BRIDGE_PORT    = if ($env:BRIDGE_PORT)     { $env:BRIDGE_PORT }     else { "5001" }
$GATEWAY_HTTP   = if ($env:GATEWAY_HTTP)    { $env:GATEWAY_HTTP }    else { "8080" }
$GATEWAY_GRPC   = if ($env:GATEWAY_GRPC)   { $env:GATEWAY_GRPC }   else { "50051" }
$GATEWAY_METRICS = if ($env:GATEWAY_METRICS) { $env:GATEWAY_METRICS } else { "9091" }
$API_KEY        = "demo-key-123"
$DEVICE         = "beaglebone-01"
$SCRIPT_DIR     = Split-Path -Parent $MyInvocation.MyCommand.Path
$GW_LOG         = "$env:TEMP\udal-gateway.log"
$BR_LOG         = "$env:TEMP\udal-bridge.log"

$GatewayProcess = $null
$BridgeProcess  = $null

# ── Farben ────────────────────────────────────────────────────────────────────
function Ok($msg)   { Write-Host "✓ $msg" -ForegroundColor Green }
function Info($msg) { Write-Host "  $msg" -ForegroundColor Cyan }
function Warn($msg) { Write-Host "! $msg" -ForegroundColor Yellow }
function Fail($msg) { Write-Host "✗ $msg" -ForegroundColor Red; Cleanup; exit 1 }
function Step($msg) { Write-Host "`n▶ $msg" -ForegroundColor Yellow }

# ── Aufräumen beim Beenden ────────────────────────────────────────────────────
function Cleanup {
    Write-Host ""
    try {
        Invoke-RestMethod -Method DELETE `
            "http://localhost:$GATEWAY_HTTP/v1/devices/$DEVICE" `
            -Headers @{"X-API-Key"=$API_KEY} -ErrorAction SilentlyContinue | Out-Null
        Warn "Device $DEVICE deregistriert"
    } catch {}
    # Schema-Deregistrierung: kein DELETE /v1/schemas in dieser UDAL-Version
    if ($BridgeProcess -and !$BridgeProcess.HasExited) {
        $BridgeProcess.Kill(); Warn "Bridge gestoppt"
    }
    if ($GatewayProcess -and !$GatewayProcess.HasExited) {
        $GatewayProcess.Kill(); Warn "Gateway gestoppt"
    }
}
Register-EngineEvent PowerShell.Exiting -Action { Cleanup } | Out-Null

function Gw-Get($path) {
    Invoke-RestMethod "http://localhost:$GATEWAY_HTTP$path" `
        -Headers @{"X-API-Key"=$API_KEY}
}
function Gw-Post($path, $body) {
    Invoke-RestMethod -Method POST "http://localhost:$GATEWAY_HTTP$path" `
        -Headers @{"X-API-Key"=$API_KEY; "Content-Type"="application/json"} `
        -Body $body
}

# ── 1. Voraussetzungen ────────────────────────────────────────────────────────
Step "Voraussetzungen prüfen"
if (!(Test-Path "$SCRIPT_DIR\udal-gateway.exe")) { Fail "udal-gateway.exe nicht gefunden" }
if (!(Get-Command python -ErrorAction SilentlyContinue) -and
    !(Get-Command python3 -ErrorAction SilentlyContinue)) {
    Fail "Python nicht installiert — https://www.python.org/downloads/"
}
$PYTHON = if (Get-Command python3 -ErrorAction SilentlyContinue) { "python3" } else { "python" }
if (!(Test-Path "$SCRIPT_DIR\beaglebone-bridge.py"))          { Fail "beaglebone-bridge.py nicht gefunden" }
if (!(Test-Path "$SCRIPT_DIR\beaglebone-bme280.schema.json")) { Fail "beaglebone-bme280.schema.json nicht gefunden" }
Ok "udal-gateway.exe, Python, bridge, schema — alles vorhanden"

# ── 2. BBB-Verbindung ─────────────────────────────────────────────────────────
Step "BBB-Verbindung testen ($BBB_URL)"
try {
    $BBB_DATA = Invoke-RestMethod "$BBB_URL/api/v1/bme280" -ErrorAction Stop
    $TEMP = [math]::Round($BBB_DATA.temperature, 2)
    Ok "BBB antwortet — aktuelle Temperatur: ${TEMP}°C"
} catch {
    Fail "BBB nicht erreichbar — USB-Kabel prüfen oder: `$env:BBB_URL='http://...' .\demo.ps1"
}

# ── 3. Gateway starten ────────────────────────────────────────────────────────
Step "UDAL Gateway starten"
$env:UDAL_DEV_INSECURE        = "true"
$env:UDAL_BOOTSTRAP_API_KEY   = "admin:admin:$API_KEY"
$env:UDAL_METRICS_ADDR        = ":$GATEWAY_METRICS"
$GatewayProcess = Start-Process -FilePath "$SCRIPT_DIR\udal-gateway.exe" `
    -RedirectStandardOutput $GW_LOG -RedirectStandardError $GW_LOG `
    -WindowStyle Hidden -PassThru

$ready = $false
for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Milliseconds 500
    try {
        Invoke-RestMethod "http://localhost:$GATEWAY_METRICS/health" -ErrorAction Stop | Out-Null
        Ok "Gateway ready  — PID $($GatewayProcess.Id)  REST :$GATEWAY_HTTP  gRPC :$GATEWAY_GRPC  metrics :$GATEWAY_METRICS"
        $ready = $true; break
    } catch {}
}
if (!$ready) { Get-Content $GW_LOG -Tail 20; Fail "Gateway nicht gestartet nach 10 s" }

# ── 4. Bridge starten ─────────────────────────────────────────────────────────
Step "Bridge starten  :$BRIDGE_PORT → $BBB_URL"
$BridgeProcess = Start-Process -FilePath $PYTHON `
    -ArgumentList "`"$SCRIPT_DIR\beaglebone-bridge.py`" --bbb-url $BBB_URL --port $BRIDGE_PORT" `
    -RedirectStandardOutput $BR_LOG -RedirectStandardError $BR_LOG `
    -WindowStyle Hidden -PassThru

$ready = $false
for ($i = 0; $i -lt 10; $i++) {
    Start-Sleep -Milliseconds 500
    try {
        Invoke-RestMethod "http://localhost:$BRIDGE_PORT/health" -ErrorAction Stop | Out-Null
        $snap = (Invoke-RestMethod "http://localhost:$BRIDGE_PORT/properties") |
            ConvertTo-Json -Compress
        Ok "Bridge ready    — PID $($BridgeProcess.Id)  GET /properties OK"
        $ready = $true; break
    } catch {}
}
if (!$ready) { Get-Content $BR_LOG -Tail 10; Fail "Bridge nicht gestartet nach 5 s" }

# ── 5. Capability-Schema registrieren ────────────────────────────────────────
Step "Capability-Schema registrieren  (POST /v1/schemas)"
$schemaBytes = [System.IO.File]::ReadAllBytes("$SCRIPT_DIR\beaglebone-bme280.schema.json")
$schemaB64   = [Convert]::ToBase64String($schemaBytes)
try {
    $resp = Gw-Post "/v1/schemas" "{`"schema`":`"$schemaB64`"}"
    $name = "$($resp.schema.name)@$($resp.schema.version)"
    Ok "Schema $name registriert"
} catch { Warn "Schema-Antwort: $_" }

# ── 6. Device registrieren ────────────────────────────────────────────────────
Step "Device registrieren  (POST /v1/devices)"
$devBody = @"
{
  "id":         "$DEVICE",
  "name":       "$DEVICE",
  "capability": "beaglebone-bme280@1.0.0",
  "transport":  "http",
  "labels": {
    "http.endpoint":      "http://localhost:$BRIDGE_PORT",
    "http.poll_interval": "5s",
    "hw":                 "beaglebone-black"
  }
}
"@
try {
    $resp = Gw-Post "/v1/devices" $devBody
    $devId = if ($resp.device.id) { $resp.device.id } else { $resp.id }
    Ok "Device registriert  — ID: $devId"
} catch { Warn "Device-Antwort: $_" }

# ── 7. Erster Property-Read ───────────────────────────────────────────────────
Step "Warte auf ersten HTTP-Adapter-Poll (5 s)..."
Start-Sleep -Seconds 6

Step "Properties lesen"
foreach ($prop in @("temperature","humidity","pressure","gpio_usr0")) {
    try {
        $r   = Gw-Get "/v1/devices/$DEVICE/properties/$prop"
        $val = ($r.value | Get-Member -MemberType NoteProperty | Select-Object -First 1 |
                ForEach-Object { $r.value.$($_.Name) })
        $unit = @{temperature="°C"; humidity="%rH"; pressure="hPa"}[$prop]
        Write-Host ("  {0,-14} {1} {2}" -f $prop, $val, $unit)
    } catch { Write-Host "  $prop  (leer)" }
}

# ── 8. Demo-Fenster ───────────────────────────────────────────────────────────
function Prop-Val($prop) {
    try {
        $r = Gw-Get "/v1/devices/$DEVICE/properties/$prop"
        $r.value | Get-Member -MemberType NoteProperty | Select-Object -First 1 |
            ForEach-Object { $r.value.$($_.Name) }
    } catch { "(leer)" }
}

function Show-Banner {
    Write-Host ""
    Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Green
    Write-Host "  Demo läuft" -ForegroundColor Green
    Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Green
    Write-Host ""
    Info "Gateway  http://localhost:$GATEWAY_HTTP   gRPC :$GATEWAY_GRPC   metrics :$GATEWAY_METRICS"
    Info "Bridge   http://localhost:${BRIDGE_PORT}/properties"
    Info "Logs     $GW_LOG   $BR_LOG"
    Write-Host ""
    Write-Host "  Befehle:" -ForegroundColor Cyan
    Write-Host "  temp        — Temperatur lesen"
    Write-Host "  hum         — Luftfeuchtigkeit lesen"
    Write-Host "  press       — Luftdruck lesen"
    Write-Host "  all         — alle Properties lesen"
    Write-Host "  devices     — registrierte Devices anzeigen"
    Write-Host "  gpio-on     — USR0 LED einschalten"
    Write-Host "  gpio-off    — USR0 LED ausschalten"
    Write-Host "  help        — diese Hilfe"
    Write-Host "  exit        — Demo beenden (Device wird deregistriert)"
    Write-Host ""
}

Show-Banner

while ($true) {
    Write-Host "  >> " -ForegroundColor Cyan -NoNewline
    $cmd = Read-Host
    switch ($cmd.Trim()) {
        { $_ -in "temp","temperature" } {
            Write-Host ("  temperature  {0} °C" -f (Prop-Val "temperature")) }
        { $_ -in "hum","humidity" } {
            Write-Host ("  humidity     {0} %rH" -f (Prop-Val "humidity")) }
        { $_ -in "press","pressure" } {
            Write-Host ("  pressure     {0} hPa" -f (Prop-Val "pressure")) }
        "all" {
            Write-Host ("  temperature  {0} °C"  -f (Prop-Val "temperature"))
            Write-Host ("  humidity     {0} %rH" -f (Prop-Val "humidity"))
            Write-Host ("  pressure     {0} hPa" -f (Prop-Val "pressure"))
            Write-Host ("  gpio_usr0    {0}"     -f (Prop-Val "gpio_usr0")) }
        "devices" {
            Gw-Get "/v1/devices" | ConvertTo-Json -Depth 5 }
        "gpio-on" {
            try {
                Invoke-RestMethod -Method POST "$BBB_URL/api/v1/gpio/60" `
                    -ContentType "application/json" -Body '{"value":1}' | Out-Null
                Ok "USR0 LED eingeschaltet"
            } catch { Warn "GPIO-Fehler: $_" } }
        "gpio-off" {
            try {
                Invoke-RestMethod -Method POST "$BBB_URL/api/v1/gpio/60" `
                    -ContentType "application/json" -Body '{"value":0}' | Out-Null
                Ok "USR0 LED ausgeschaltet"
            } catch { Warn "GPIO-Fehler: $_" } }
        { $_ -in "help","?" } { Show-Banner }
        { $_ -in "exit","quit","q" } { break }
        ""  {}
        default { Warn "Unbekannt: '$cmd' — 'help' für Befehle" }
    }
    if ($cmd.Trim() -in "exit","quit","q") { break }
}

Cleanup
