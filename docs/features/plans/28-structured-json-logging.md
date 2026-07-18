# Plan: #28 — Structured JSON Logging

## Ausgangslage

F-23. `docs/req42/req42.adoc` F-23 und `docs/arc42/arc42.adoc` §8.2 hatten
bereits vollständige Spec-Doku (Pflichtfelder, `UDAL_LOG_LEVEL`,
`trace_id` == OTEL-Span-ID), aber keine Implementierung — der Gateway
loggte bislang über einen einzigen `slog.NewTextHandler(os.Stdout, nil)`
(`cmd/gateway/main.go`), unstrukturiert, ohne `component`/`trace_id`, und
ohne jede Laufzeit-Konfigurierbarkeit des Levels.

F-24 (OpenTelemetry Tracing, issue #29) — von F-23s dritter AC referenziert
("`trace_id` matching the OTEL span ID") — ist selbst noch nicht
implementiert. Design-Entscheidungen unten erklären, wie F-23 trotzdem
sinnvoll und vorwärtskompatibel umgesetzt wurde, ohne auf #29 zu warten.

## Design-Entscheidungen

- **`UDAL_LOG_LEVEL=debug ... ohne Neustart` wörtlich nicht möglich** — ein
  laufender Prozess sieht Änderungen an seinen eigenen Umgebungsvariablen
  grundsätzlich nicht, ohne neu gestartet (re-exec't) zu werden; das gilt
  unabhängig von Go. Umgesetzt wurde stattdessen: `UDAL_LOG_LEVEL` setzt
  das Level, mit dem ein *neu gestarteter* Gateway beginnt (erfüllt den
  Beispielbefehl der AC im Normalfall direkt), und ein neuer Endpoint `PUT
  /debug/log-level` (Body: `debug`\|`info`\|`warn`\|`error`) auf dem
  Metrics-Listener ändert das Level eines *bereits laufenden* Prozesses
  live — das ist der tatsächlich funktionierende "ohne Neustart"-Pfad. `GET
  /debug/log-level` liest das aktuelle Level zurück. Verifiziert per
  manuellem Smoke-Test: Gateway mit `UDAL_LOG_LEVEL=warn` gestartet (keine
  INFO-Zeilen), `PUT .../debug/log-level` mit `debug` → nachfolgende
  DEBUG-Zeilen erscheinen, ohne den Prozess neu zu starten.
- **`metrics_port`/`UDAL_METRICS_PORT` erstmals verdrahtet** (bislang seit
  #41 nur geparst, ungenutzt) — für `/debug/log-level`, nicht für
  `/health`/`/metrics` selbst (das bleibt #27). Der neue `*http.Server`
  nutzt einen `http.ServeMux`, damit #27 `/health` und `/metrics` auf
  demselben Mux/Port ergänzen kann, ohne diesen Ticket-Zuschnitt zu
  verletzen.
- **`trace_id` vorwegnehmend im OTEL-`TraceID`-Format generiert** (16
  Zufallsbytes, hex-kodiert, W3C Trace Context / OpenTelemetry-Konvention),
  nicht auf #29 gewartet: Ein neuer `logging.Interceptor` läuft als erster
  in der gRPC-Interceptor-Kette (vor `auth.Authenticator`, damit auch eine
  fehlgeschlagene Authentifizierung eine `trace_id` und eine geloggte
  Request-Zeile bekommt) und legt die ID in den Request-Context. Jeder
  Log-Aufruf, der mit diesem Context arbeitet (`*Context`-Methoden von
  `log/slog`), bekommt sie automatisch angehängt — über einen
  `contextHandler`, der `slog.Handler` umschließt (das dokumentierte
  Standardmuster für kontext-getragene Log-Attribute). Wenn #29 landet,
  muss dort nur der `GenerateTraceID()`-Aufruf durch das Auslesen der
  echten OTEL-Span-`TraceID` ersetzt werden — gleiches Format, gleicher
  Log-Key, keine nachgelagerte Änderung nötig. F-23s dritte AC
  ("`trace_id` matching the OTEL span ID") bleibt daher bewusst
  **unchecked** in `req42.adoc` — es gibt noch keine echte OTEL-Span, mit
  der die `trace_id` übereinstimmen könnte; das ist ehrlich als "Interim,
  pending F-24" dokumentiert statt fälschlich als erledigt markiert.
- **`component` per abgeleitetem Kind-Logger, nicht per Handler-Logik**:
  `main.go` hält einen `baseLog` ohne `component`-Attribut und leitet für
  jedes Subsystem einen eigenen `.With("component", "…")`-Logger ab
  (`mqtt_adapter`, `http_adapter`, `capability_registry`, `gateway.api`,
  sowie `gateway` für main.go selbst) — der Standard-`slog`-Weg für ein
  festes Pro-Logger-Attribut, erfordert keine Änderung an den
  Adapter-Paketen selbst (die akzeptieren bereits `WithLogger(*slog.Logger)`
  seit #11/#22/#24).
- **Bestehende Ad-hoc-Log-Keys (`err`, `device`) nicht umbenannt**: F-23s
  Pflichtfelder sind `timestamp`/`level`/`component`/`trace_id`; `device_id`
  und `error` sind explizit *optional*. Eine flächendeckende Umbenennung
  vorhandener `log.Error(..., "err", err)`-Aufrufe in allen
  Adapter-Paketen wäre ein großer, rein kosmetischer Diff ohne AC-Bezug —
  bewusst nicht Teil dieses Tickets.
- **Keine neue `gateway.yaml`-Config-Option für das Log-Level**: F-23s AC
  nennt ausschließlich `UDAL_LOG_LEVEL` als Env-Var; ein zusätzlicher
  YAML-Key wurde nicht spezifiziert und daher nicht ergänzt (Scope-Diät,
  analog zu vorherigen Tickets).

## Testabdeckung

- **`internal/logging`**: `level_test.go` (Parsing inkl. Fehlerfall),
  `trace_test.go` (ID-Form/Eindeutigkeit, Context-Roundtrip),
  `handler_test.go` (Pflichtfelder inkl. `timestamp`-Umbenennung,
  `trace_id` nur bei vorhandenem Request-Context, Level-Gating,
  Kompatibilität mit `.With(...)`-Ableitung), `interceptor_test.go`
  (Unary/Stream, genau eine Zeile pro Request, `trace_id` im Handler-Context
  sichtbar, Status-Code-Mapping inkl. Nicht-`status`-Fehler),
  `debug_handler_test.go` (GET/PUT/ungültiges Level/Methode nicht erlaubt).
- **Manueller End-to-End-Smoke-Test** (kein automatisierter Test, da er
  einen echten Prozessstart braucht): Gateway mit `UDAL_LOG_LEVEL=debug`
  gestartet → jede Startup-Zeile ist gültiges JSON mit
  `timestamp`/`level`/`component`; mit `UDAL_LOG_LEVEL=warn` gestartet →
  keine INFO-Zeilen; `GET`/`PUT /debug/log-level` live gegen den
  laufenden Prozess verifiziert (Level tatsächlich geändert, keine
  INFO-Unterdrückung mehr nach dem `PUT`).

`go build ./...`, `go vet ./...`, `gofmt -l .`, `go test -race ./...` und
`golangci-lint run` sind grün.
