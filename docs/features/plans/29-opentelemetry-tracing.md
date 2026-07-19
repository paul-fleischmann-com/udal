# Plan: #29 — OpenTelemetry Distributed Tracing

## Ausgangslage

F-24. `docs/req42/req42.adoc` F-23 (issue #28, structured JSON logging)
hatte bereits ein OTEL-`TraceID`-förmiges `trace_id`-Feld auf jeder
Request-Log-Zeile — selbst generiert (16 Zufallsbytes, hex-kodiert), ohne
eine echte OpenTelemetry-Span dahinter, explizit "Interim, pending F-24"
dokumentiert. F-24 selbst (dieses Ticket) ersetzt diese
Platzhalter-Generierung durch eine echte OTel-`TracerProvider`/Span-Kette
und macht `trace_id` erstmals tatsächlich mit einer OTEL-Span-ID
übereinstimmend, wie F-23s dritte AC von Anfang an verlangte.

## Design-Entscheidungen

- **`TracerProvider` wird immer real gebaut, nie no-op** — unabhängig davon,
  ob `UDAL_OTEL_ENDPOINT` gesetzt ist. `sdktrace.NewTracerProvider`s
  Default-Sampler (`ParentBased(AlwaysSample)`) und Default-ID-Generator
  erzeugen eine echte, zufällige Trace-ID für jede Span auch ganz ohne
  angehängten Exporter. Das ist bewusst so gewählt: F-23s "jede
  Request-Log-Zeile hat eine `trace_id`" darf durch F-24s "tracing disabled
  if unset" nicht kaputtgehen — "disabled" bedeutet hier ausschließlich,
  dass kein OTLP-Netzwerkverkehr den Prozess verlässt, nicht dass die
  Request-Korrelation aufhört zu funktionieren. Nur wenn
  `UDAL_OTEL_ENDPOINT` nicht-leer ist, wird ein echter OTLP/gRPC-Exporter
  per `WithBatcher` angehängt; sonst hat der `TracerProvider` gar keinen
  Span-Processor — Spans werden erzeugt, für Context-Propagation und
  Logging genutzt und dann einfach verworfen, ohne Netzwerk-I/O.
- **`UDAL_OTEL_ENDPOINT` als reine Env-Var, kein neuer `gateway.yaml`-Key**
  — F-24s AC nennt ausschließlich diese eine Env-Var; analog zu
  `UDAL_DEV_INSECURE`/`UDAL_BOOTSTRAP_API_KEY`/`UDAL_CAPABILITY_ENFORCEMENT`
  wird sie direkt per `os.Getenv` gelesen, ohne über `internal/config`s
  YAML-Override-Mechanismus zu gehen (Scope-Diät, analog zu #28).
  Endpoint-Form ist doppelt unterstützt: ein blankes `host:port` (üblicher
  lokaler Collector, z. B. `otel-collector:4317`) wird als Klartext-gRPC
  interpretiert, eine volle URL mit Schema (`https://collector:4317`) über
  `WithEndpointURL` (impliziert TLS bei `https://`).
- **Interceptor-Reihenfolge: `tracing.Interceptor` zuerst, vor
  `logging.Interceptor` und `auth.Authenticator`** — beide lesen die von
  ihm gesetzte Span aus `ctx`: `logging.Interceptor`/`contextHandler` für
  die `trace_id` der Request-Log-Zeile (auch bei fehlgeschlagener Auth),
  `auth.Authenticator` um seine eigene `"auth"`-Span als Kind der
  `"api"`-Span zu parenten. `requestMetrics` (issue #27) bleibt an
  unveränderter Position, da es `ctx` nicht anfasst.
- **Span-Baum pro Request**: `"api"` (`tracing.Interceptor`, deckt sowohl
  gRPC als auch die per grpc-gateway proxyte REST-Schnittstelle ab) →
  `"auth"` (`auth.Authenticator.authenticateTraced`) als *Geschwister*-Kind,
  nicht als Vorfahre von `"router"`: die `"auth"`-Span wird bewusst mit
  einem separaten `spanCtx` gestartet und beendet, bevor der *ursprüngliche*
  `ctx` (nicht `spanCtx`) an `ContextWithIdentity`/den Handler weitergereicht
  wird — sonst würde `"router"` unter einer bereits beendeten `"auth"`-Span
  hängen, statt korrekt als nächstes Kind von `"api"`. `"router"`
  (`DeviceService.GetProperty`/`SetProperty`) → `"adapter"`
  (mqtt/http/can-Dispatch) nur für diese beiden RPCs und nur, wenn
  tatsächlich zu einem Transport-Adapter geroutet wird — der
  `PropertyStore`-Fallback (kein Adapter konfiguriert) bekommt eine
  `"router"`-Span, aber keine `"adapter"`-Span, da kein Adapter-Aufruf
  stattfindet. Alle anderen RPCs (`ListDevices`, `RegisterDevice`, ...)
  bekommen weder `"router"` noch `"adapter"` — sie haben keine
  Transport-Adapter-Dispatch-Logik, die eine eigene Span rechtfertigt.
- **Ein gemeinsamer `startSpan`-Helper in `service.DeviceService`** statt
  vier fast identischer `otel.Tracer(...).Start`/`RecordError`/`SetStatus`/
  `End`-Blöcke — reduziert jede der vier Stellen (Router-Span,
  mqtt/http/can-Adapter-Span) auf einen Einzeiler plus `defer`.
- **`logging.Interceptor` verliert seine eigene Trace-ID-Erzeugung
  vollständig** — sie war seit #28 nur ein vorwegnehmender Platzhalter.
  `contextHandler` (handler.go) bevorzugt jetzt die echte OTel-Span aus
  `ctx` (`trace.SpanContextFromContext`); die alte
  `TraceIDFromContext`-Mechanik bleibt als Fallback für Kontexte ohne aktive
  Span bestehen (z. B. ein Hintergrund-Goroutine-Aufruf, der die
  Interceptor-Kette umgeht), wird aber im Normalfall nie mehr getroffen.
- **Graceful Shutdown flusht den `TracerProvider`**: `tp.Shutdown(shutCtx)`
  läuft im bestehenden 5-Sekunden-Shutdown-Fenster, nach den
  HTTP-Server-Shutdowns — damit noch im Batch-Processor gepufferte Spans
  vor Prozessende exportiert werden, statt stillschweigend verworfen zu
  werden.

## Testabdeckung

- **`internal/tracing`**: `provider_test.go` (echte Trace-IDs auch ohne
  Endpoint, `host:port`- und URL-Form des Exporters, unterschiedliche Spans
  teilen keine Trace-ID sofern nicht verwandt), `interceptor_test.go`
  (`"api"`-Span wird gestartet, Fehler-Status bei Handler-Fehler inkl.
  Nicht-`status`-Fehler, Stream-Variante wrapped den Context korrekt).
- **`internal/auth`**: zwei neue Tests
  (`TestUnaryInterceptor_ValidAPIKey_RecordsAuthSpan`/
  `..._InvalidAPIKey_RecordsErrorOnAuthSpan`) verifizieren per
  `tracetest.InMemoryExporter`, dass `authenticateTraced` genau eine
  `"auth"`-Span erzeugt und deren Status bei fehlgeschlagener
  Authentifizierung auf `Error` steht.
- **`internal/service`**: drei neue Tests in
  `device_service_tracing_test.go` verifizieren Router-/Adapter-Span-Baum
  für den mqtt-Erfolgspfad (inkl. Parent-Child-Beziehung über
  `SpanContext`/`Parent`), Fehler-Status auf beiden Spans bei einem
  Adapter-Fehler, und dass der `PropertyStore`-Fallback nur eine
  `"router"`-, keine `"adapter"`-Span erzeugt.
- **`internal/logging`**: `interceptor_test.go` umgeschrieben — die beiden
  alten Tests, die `TraceIDFromContext` direkt prüften, sind ersetzt durch
  Tests, die eine echte OTel-Span vorab in den Context legen (simuliert, was
  `tracing.Interceptor` in Produktion tut) und verifizieren, dass die
  geloggte `trace_id` exakt der Span-Trace-ID entspricht — plus einen neuen
  Test, der zeigt, dass ganz ohne aktive Span kein `trace_id`-Feld mehr
  geloggt wird (`logging.Interceptor` erzeugt selbst keins mehr).
- **Manueller End-to-End-Smoke-Test** (kein automatisierter Test, da er
  einen echten Prozessstart braucht): Gateway mit `UDAL_DEV_INSECURE=true`
  und ohne `UDAL_OTEL_ENDPOINT` gestartet, `RegisterDevice`/`SetProperty`/
  `GetProperty` per REST aufgerufen → jede Request-Log-Zeile trägt eine
  eigene, gültige 32-stellige Hex-`trace_id` (bestätigt: echte
  `tracing.Interceptor`-Verdrahtung durch `main.go` funktioniert end-to-end,
  nicht nur isoliert in Unit-Tests) — verifiziert das exakte Verhalten, das
  F-23s dritte AC jetzt als erledigt markiert.

`go build ./...`, `go vet ./...`, `gofmt -l .` und `go test -race ./...`
sind grün.
