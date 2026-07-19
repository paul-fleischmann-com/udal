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

## Nachgezogen aus dem Review (vor PR-Eröffnung)

Ein High-Effort-Multi-Agent-Review (8 Finder-Winkel + Verifikation) lief
gegen den vollständigen Diff, bevor die PR eröffnet wurde. Ein Finding —
unabhängig von fünf der acht Finder-Winkel gefunden — war ein echter
Korrektheitsfehler und wurde behoben; zwei kleinere Duplikations-Findings
wurden ebenfalls behoben, der Rest (Interceptor-Reihenfolge nur per
Kommentar statt Test abgesichert, geteiltes 5s-Shutdown-Deadline-Budget,
zwei strukturell identische ctx-wrappende Stream-Typen) bewusst als
Low-Severity/bestehende Konvention belassen:

- **`routeErr` wurde nicht auf jedem Fehlerpfad gesetzt, der innerhalb der
  bereits offenen `"router"`-Span lief** (höchste Schwere, unabhängig von
  fünf Review-Winkeln gefunden): `GetProperty`/`SetProperty` fädelten
  ursprünglich eine separate `routeErr`-Variable in den deferred
  `endRouterSpan`-Aufruf, aber nur die drei Adapter-Branches (mqtt/http/can)
  setzten sie tatsächlich. Der `PropertyStore`-Fallback (kein Adapter
  konfiguriert — der häufigste Fall), der `toProtoValue`-Encode-Fehlerpfad
  in `GetProperty`, sowie `SetProperty`s explizites HTTP-Unimplemented und
  sein eigener `PropertyStore.Set`-Fallback gaben allesamt einen Fehler an
  den Client zurück, ohne `routeErr` je zu setzen — die `"router"`-Span
  meldete in all diesen Fällen fälschlich Erfolg. In einem Trace-Backend
  (Jaeger/Tempo) hätte das genau die Anfragen, für die F-24s Tracing
  eigentlich Fehler sichtbar machen soll, als grüne, gesunde Spans gezeigt.
  Fix: `GetProperty`/`SetProperty` nutzen jetzt benannte Rückgabewerte
  (`resp`, `err`) und `defer func() { endRouterSpan(err) }()` — jede
  `return`-Anweisung, auch die bislang übersehenen, setzt automatisch den
  tatsächlichen Rückgabefehler, den die Span sieht; die separate
  `routeErr`-Variable entfällt komplett, ein struktureller statt
  Pflaster-Fix. Zwei neue Regressionstests
  (`TestGetProperty_PropertyStoreFallbackError_RecordsErrorOnRouterSpan`,
  `TestSetProperty_HTTPUnimplemented_RecordsErrorOnRouterSpan`) verifizieren
  genau die beiden am leichtesten reproduzierbaren der vier betroffenen
  Pfade (die übrigen zwei — `toProtoValue`-Encode-Fehler,
  `PropertyStore.Set`-Fehler — sind mit den vorhandenen In-Memory-Test-
  Fakes nicht ohne Weiteres provozierbar, sind aber durch denselben
  strukturellen Fix mitbehoben).
- **Span-Fehler-Aufzeichnung dreifach unabhängig reimplementiert, mit
  Message-Format-Drift** (zwei Review-Winkel unabhängig gefunden):
  `tracing.Interceptor`s `recordResult`, `auth.authenticateTraced` und
  `service.startSpan` bauten je ihre eigene
  `span.RecordError`+`span.SetStatus`-Sequenz — `recordResult` nutzte dabei
  `grpcstatus.Convert(err).Message()` (reiner Message-Text), die anderen
  beiden `err.Error()` (vollständiger `"rpc error: code = ... desc = ..."`-
  String) — eine stille Inkonsistenz, die kein Test abdeckte. Fix: neue
  exportierte `tracing.RecordError(span, err)`, von allen drei Stellen
  genutzt, vereinheitlicht auf `err.Error()`.
- **Uncached `otel.Tracer(name)`-Lookups pro Span** wurden zunächst als
  Efficiency-Fix mit einer paketweiten `var tracer = otel.Tracer(...)`
  behoben, dann aber wieder verworfen: `go test` deckte auf, dass OTel's
  globaler Delegations-Mechanismus (`internal/global`) einen bereits
  vergebenen `Tracer`-Proxy nur beim *ersten* `otel.SetTracerProvider`-Aufruf
  pro Prozess umhängt (`sync.Once` intern) — jeder Test, der pro Testfall
  einen frischen `TracerProvider` registriert (das etablierte Muster in
  `internal/tracing`, `internal/auth` und `internal/service`s Span-Tests),
  hätte mit einem gecachten Tracer-Handle nur beim allerersten `SetTracer
  Provider`-Aufruf im gesamten Testbinary tatsächlich Spans aufgezeichnet
  — alle späteren Testfälle hätten still leere Exporter gesehen. Da dasselbe
  Muster potenziell auch in einer zukünftigen Produktions-Situation mit
  mehrfachem `SetTracerProvider` (z. B. Hot-Reload der Tracing-Config)
  denselben Fehler hätte, wurde die Optimierung verworfen statt nur für
  Tests umgangen — `otel.Tracer(name)` bleibt bewusst ein Aufruf pro Span.
