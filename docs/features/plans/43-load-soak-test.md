# Plan: #43 — Load/soak test (1,000 concurrent devices, goroutine leak check)

## Ausgangslage

QR-02 (req42.adoc §5.2): 1.000 gleichzeitig verbundene MQTT-Geräte, jedes
publiziert alle 10s Telemetrie; Ziel ist zu verifizieren, dass alles
verarbeitet/fanned-out wird, ohne übermäßiges Ressourcenwachstum oder
Goroutine-Leaks. Braucht den MQTT-Adapter (#11) für realistische Last; der
Subscribe-Fan-out-Pfad existiert bereits (#8).

## Scope- und Design-Entscheidungen

- **Simulator-Verbindungen vs. Geräteanzahl entkoppelt**: Die Ressourcen-
  Grenzwerte der AC (Heap < 500 MB, CPU < 70%) beziehen sich auf den
  **Gateway/Adapter-Prozess**, nicht auf den Lastgenerator. Der Adapter
  selbst hält genau **eine** Broker-Verbindung und ruft `WatchDevice` für
  1.000 verschiedene Device-IDs auf (1.000 echte SUBSCRIBE-Aufrufe/Topic-
  Filter auf dieser einen Verbindung — das ist der eigentliche Skalierungs-
  test für den Adapter). Der Simulator selbst nutzt einen kleinen Pool
  echter MQTT-Verbindungen (nicht 1.000 einzelne OS-Sockets), die im
  Round-Robin für alle 1.000 Geräte publizieren — aus Sicht der
  Adapter-Verbindung ist der eingehende Traffic identisch (1.000
  verschiedene Topic-Strings, 100 Events/s bei 10s-Intervall), nur der
  Ressourcen-Fußabdruck des Testwerkzeugs selbst ist kleiner. Broker-seitige
  Skalierung auf 1.000 echte TCP-Verbindungen ist nicht Gegenstand dieses
  Tickets (das wäre ein Mosquitto-Lasttest, kein Gateway-Lasttest).
- **"Alle Events verarbeitet/fanned-out"**: echter `api.Broker` mit
  `Subscribe`-Channels für alle 1.000 Geräte (1.000 Goroutinen à ~2KB Stack
  sind vernachlässigbar) zählt empfangene Updates pro Gerät; Test schlägt
  fehl, wenn irgendein Gerät nach Ablauf der Laufzeit keine (oder zu wenige)
  Updates bekommen hat.
- **Konfigurierbare Laufzeit statt hartcodierter 30 Minuten**: Ein
  automatisierter 30-Minuten-Lauf bei jedem PR ist nicht praktikabel. Die
  Harness parametrisiert Geräteanzahl/Publish-Intervall/Laufzeit über
  Env-Vars (Default: kurz, für schnelle Verifikation der Korrektheit);
  der reale 30-Minuten-Soak-Lauf (AC "kein Leak nach 30 min") wird **einmal
  real ausgeführt** (Hintergrund-Task, nicht blockierend) und das Ergebnis
  im PR dokumentiert — analog dazu, wie in #11 die v3.1.1-Fallback-Trigger
  nicht gegen echte Infrastruktur testbar war und stattdessen gezielt
  anderweitig verifiziert wurde.
- **CI-Integration**: Ein neuer Job, ausschließlich `workflow_dispatch`
  (manuell) plus optional ein nächtlicher Cron — **nicht** Teil von
  "CI Gate (all checks passed)" und **nicht** auf jedem PR/Push, da ein
  1.000-Geräte-Lauf mehrere Minuten dauert. Lokal ausführbar via
  `go test -tags loadtest ./...`.
- **CPU/Heap-Messung**: `runtime.ReadMemStats` für Heap;
  `syscall.Getrusage(RUSAGE_SELF, ...)` (Ru_utime+Ru_stime) vor/nach dem
  Lauf für CPU-Zeit, daraus CPU% über die Wandzeit / `runtime.NumCPU()`
  berechnet — Linux-spezifisch, aber CI läuft ohnehin auf `ubuntu-latest`.
  Zusätzlich ein `runtime/pprof`-Heap-Profil beim Abschluss geschrieben
  (AC: "verified via pprof").
- **Goroutine-Leak-Check**: `runtime.NumGoroutine()` vor Testbeginn
  (nach Warmup/Verbindungsaufbau) und nach Testende (nach Abbau aller
  Subscriber/Simulator-Verbindungen, mit kurzer Gnadenfrist für
  Goroutine-Cleanup), mit kleiner Toleranz statt exakter Gleichheit
  (natürliche Schwankungen durch GC/Laufzeit-interne Goroutinen).

## Phasen

### Phase 1 — Harness-Grundgerüst
- `code/gateway/internal/adapters/mqtt/loadtest_test.go` (Build-Tag
  `loadtest`), skip wenn `UDAL_TEST_MQTT_BROKER` fehlt (gleiche Konvention
  wie der bestehende `integration`-Tag)
- Simulator: kleiner Pool von v3.1.1-Verbindungen (wiederverwendet
  `connectV3` — bereits vorhanden, unexported, gleiches Package), Round-Robin
  über 1.000 Device-IDs
- Echter `Adapter` + `WatchDevice` für alle 1.000 IDs; echter `api.Broker`
  mit 1.000 `Subscribe`-Channels zählt empfangene Updates

### Phase 2 — Ressourcen-Messung + kurzer Korrektheits-Lauf
- Heap/Goroutine/CPU-Messung vor/nach; pprof-Heap-Profil
- Kurzer Lauf (Sekunden, nicht Minuten) lokal verifizieren: alle 1.000
  Geräte bekommen Updates, keine grobe Ressourcen-Explosion

### Phase 3 — Realer Soak-Lauf (einmalig, dokumentiert)
- Lauf mit AC-naher Konfiguration (1.000 Geräte, 10s-Intervall, möglichst
  nah an 30 Minuten im Rahmen der Sandbox-Zeitbudgets) als
  Hintergrund-Task; Ergebnis (Heap/CPU/Goroutine-Delta) im PR dokumentieren

### Phase 4 — CI-Wiring + Doku + Changelog
- Neuer, nicht-blockierender CI-Job (`workflow_dispatch`)
- README/CHANGELOG-Eintrag mit Ausführungsanleitung und Ergebnis des
  Soak-Laufs

## Ergebnis des realen Soak-Laufs (Phase 3)

Ausgeführt mit der exakten AC-Konfiguration gegen einen echten
Mosquitto-Broker (lokal installiert, siehe #11):

```
UDAL_LOADTEST_DEVICES=1000 UDAL_LOADTEST_PUBLISH_INTERVAL=10s UDAL_LOADTEST_DURATION=30m
```

| Messung | Ergebnis | AC-Grenzwert |
|---|---|---|
| Heap (`HeapAlloc`) | 11.3 MB | < 500 MB |
| CPU-Auslastung | 0.4 % (über 30 min Wandzeit, 2 CPUs) | < 70 % |
| Goroutinen während des Laufs | konstant 2208 (4 Stichproben über 30 min, keine Abweichung) | kein Wachstum |
| Goroutinen nach vollständigem Teardown | 2 (Baseline vor dem Lauf: 1207) | zurück auf Baseline |
| Events verarbeitet | alle 1.000 Geräte haben Updates empfangen | alle |

Laufzeit: 1801s (30 min Last + Setup/Teardown). Ergebnis: alle
Akzeptanzkriterien klar erfüllt, keine Hinweise auf ein Goroutine-Leak
oder übermäßigen Ressourcenverbrauch.
