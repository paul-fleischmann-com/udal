<#
.SYNOPSIS
  Builds, starts, and verifies the UDAL docker-compose demonstrator stack
  (issue #30) end-to-end: register the simulated device, read/write a
  property, and confirm it reports online — the same steps as
  examples/README.adoc, scripted and using Invoke-RestMethod instead of
  curl so there's no shell-quoting to get wrong on Windows.

.PARAMETER Down
  Tear the stack down after verification instead of leaving it running.

.PARAMETER TimeoutSeconds
  How long to wait for the gateway to report ready before giving up.

.EXAMPLE
  .\verify-demo.ps1
.EXAMPLE
  .\verify-demo.ps1 -Down
#>
param(
    [switch]$Down,
    [string]$ApiKey = "demo-api-key",
    [string]$DeviceId = "sim-temp-1",
    [int]$TimeoutSeconds = 60
)

$ErrorActionPreference = "Stop"

function Write-Step { param($msg) Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Ok   { param($msg) Write-Host "    OK: $msg" -ForegroundColor Green }
function Write-Fail { param($msg) Write-Host "    FAIL: $msg" -ForegroundColor Red }

# This script lives in examples/, one level below the repo root.
$repoRoot = Split-Path -Parent $PSScriptRoot
$composeDir = Join-Path $repoRoot "deployments\docker"

if (Get-Command docker -ErrorAction SilentlyContinue) {
    $composeExe = "docker"
} elseif (Get-Command podman -ErrorAction SilentlyContinue) {
    $composeExe = "podman"
} else {
    Write-Fail "Neither 'docker' nor 'podman' found on PATH."
    exit 1
}
Write-Step "Using compose provider: $composeExe compose"

Push-Location $composeDir
try {
    Write-Step "Building and starting the stack..."
    & $composeExe compose up --build -d
    if ($LASTEXITCODE -ne 0) { throw "compose up failed (exit $LASTEXITCODE)" }
    Write-Ok "containers started"

    Write-Step "Waiting for the gateway to report ready (up to ${TimeoutSeconds}s)..."
    $health = $null
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $health = Invoke-RestMethod -Uri "http://localhost:9090/health" -TimeoutSec 3
            if ($health.status -eq "ok") { break }
        } catch { }
        Start-Sleep -Seconds 1
        $health = $null
    }
    if (-not $health) { throw "gateway did not become ready within ${TimeoutSeconds}s" }
    Write-Ok "gateway ready: $($health | ConvertTo-Json -Compress)"

    Write-Step "Container states:"
    & $composeExe compose ps

    Write-Step "Registering device '$DeviceId'..."
    $headers = @{ "X-API-Key" = $ApiKey }
    $registerBody = @{
        id         = $DeviceId
        name       = "Simulated Temperature Sensor"
        capability = "temperature-sensor"
        transport  = "mqtt"
    } | ConvertTo-Json
    try {
        $device = Invoke-RestMethod -Method Post -Uri "http://localhost:8080/v1/devices" `
            -Headers $headers -ContentType "application/json" -Body $registerBody
        Write-Ok "registered: $($device | ConvertTo-Json -Compress)"
    } catch {
        # A re-run against an already-registered device is fine — anything
        # else (bad API key, gateway not actually up, ...) should still fail loudly.
        if ($_.Exception.Response -and $_.Exception.Response.StatusCode.value__ -eq 409) {
            Write-Ok "device already registered from a previous run (409) — continuing"
        } else {
            throw
        }
    }

    Write-Step "Waiting for a live temperature reading (simulator publishes every few seconds)..."
    $prop = $null
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-Date) -lt $deadline) {
        try {
            $prop = Invoke-RestMethod -Uri "http://localhost:8080/v1/devices/$DeviceId/properties/temperature" `
                -Headers $headers -TimeoutSec 3
            if ($null -ne $prop.value.floatVal) { break }
        } catch { }
        Start-Sleep -Seconds 2
        $prop = $null
    }
    if (-not $prop) { throw "no temperature value became available within 30s" }
    Write-Ok "temperature = $($prop.value.floatVal)"

    Write-Step "Writing sample_interval_s = 10..."
    $writeBody = @{ intVal = "10" } | ConvertTo-Json
    $writeResp = Invoke-RestMethod -Method Put -Uri "http://localhost:8080/v1/devices/$DeviceId/properties/sample_interval_s" `
        -Headers $headers -ContentType "application/json" -Body $writeBody
    if ($writeResp.newValue.intVal -ne "10") {
        throw "write did not round-trip: $($writeResp | ConvertTo-Json -Compress)"
    }
    Write-Ok "sample_interval_s = $($writeResp.newValue.intVal)"

    Write-Step "Checking device status..."
    # GetDevice's response is { "device": {...} } (v1GetDeviceResponse) — the
    # device fields are nested under .device, not at the top level.
    $listed = Invoke-RestMethod -Uri "http://localhost:8080/v1/devices/$DeviceId" -Headers $headers
    $status = $listed.device.status
    if ($status -eq "DEVICE_STATUS_ONLINE") {
        Write-Ok "device status: $status"
    } else {
        Write-Fail "device status: '$status' (expected DEVICE_STATUS_ONLINE — the heartbeat may not have landed yet; try again in a few seconds)"
    }

    if (Get-Command grpcurl -ErrorAction SilentlyContinue) {
        Write-Step "Subscribe smoke test via grpcurl (6s, expecting at least one event)..."
        $job = Start-Job -ScriptBlock {
            param($apiKey, $deviceId)
            & grpcurl -plaintext -H "x-api-key: $apiKey" -d "{`"deviceId`": `"$deviceId`"}" localhost:50051 udal.v1.DeviceService/Subscribe
        } -ArgumentList $ApiKey, $DeviceId
        Start-Sleep -Seconds 6
        $output = Receive-Job $job
        Stop-Job $job -ErrorAction SilentlyContinue
        Remove-Job $job -ErrorAction SilentlyContinue
        if ($output) { Write-Ok "received a Subscribe event" } else { Write-Fail "no Subscribe event received in 6s" }
    } else {
        Write-Step "grpcurl not found on PATH — skipping the Subscribe smoke test (optional; see examples/README.adoc step 5)"
    }

    Write-Host ""
    Write-Host "Demo verified successfully." -ForegroundColor Green
    Write-Host "  Gateway REST:   http://localhost:8080"
    Write-Host "  Gateway gRPC:   localhost:50051"
    Write-Host "  Health/metrics: http://localhost:9090/health"
}
catch {
    Write-Fail $_.Exception.Message
    Write-Host ""
    Write-Host "Recent gateway logs:" -ForegroundColor Yellow
    & $composeExe compose logs gateway --tail 50
    Write-Host ""
    Write-Host "Recent device-simulator logs:" -ForegroundColor Yellow
    & $composeExe compose logs device-simulator --tail 20
    exit 1
}
finally {
    if ($Down) {
        Write-Step "Tearing down (-Down was set)..."
        & $composeExe compose down
    }
    Pop-Location
}
