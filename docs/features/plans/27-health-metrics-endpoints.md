# Plan: #27 — Health + Prometheus Metrics Endpoints

## Ausgangslage

F-21/F-22. `docs/req42/req42.adoc` (beide Features) und `docs/arc42/arc42.adoc`
§8.3/§5.2 ("Health Monitor"-Baustein: "Go, prometheus/client_golang",
"Heartbeat tracking; online/offline events; /health + /metrics endpoints")
hatten bereits vollständige, zutreffende Spec-/Architektur-Doku — keine
Korrektur nötig, anders als bei #24/#25, wo Technologie-Angaben erst
nachgezogen werden mussten. Es gab weder einen `/health`- noch einen
`/metrics`-Endpoint; `adapters.metrics_port`/`UDAL_METRICS_PORT` war seit
#41 ein geparster, aber komplett ungenutzter Config-Stub — #28
(strukturiertes Logging) hatte kurz zuvor einen ersten echten Listener
dafür angelegt (`/debug/log-level`), auf dem #27 aufbaut.

**Branch-Historie:** Der Branch für #27 wurde ursprünglich von #28s
(damals noch offenem) Branch abgezweigt, bevor sowohl #25 (CAN-Adapter,
PR #60) als auch #28 (PR #61) gemerged wurden — beide mussten per
`git rebase` nachgezogen werden, inklusive Konfliktauflösung in
`CHANGELOG.md` und `cmd/gateway/main.go` (beide PRs fügten unabhängig
Abschnitte an derselben Stelle ein — CAN-Adapter-Setup und
Metrics/Debug-Listener direkt vor dem Auth-Abschnitt).

## Design-Entscheidungen

- **`UDAL_LOG_LEVEL=debug ... ohne Neustart` wörtlich nicht möglich** — ein
  laufender Prozess sieht Änderungen an seinen eigenen Umgebungsvariablen
  grundsätzlich nicht, ohne neu gestartet (re-exec't) zu werden; das gilt
  unabhängig von Go. Umgesetzt wurde stattdessen: `UDAL_LOG_LEVEL` setzt
  das Level, mit dem ein *neu gestarteter* Gateway beginnt (erfüllt den
  Beispielbefehl der AC im Normalfall direkt), und ein neuer Endpoint `PUT
  /debug/log-level` (Body: `debug`\|`info`\|`warn`\|`error`) auf dem
  Metrics-Listener ändert das Level eines *bereits laufenden* Prozesses
  live — das ist der tatsächlich funktionierende "ohne Neustart"-Pfad.
- **Health/Metrics auf demselben Listener wie `/debug/log-level`** (#28):
  Kein zweiter HTTP-Server, ein `http.ServeMux` für alle drei Admin-Routen.
- **`health.Reporter`-Interface statt fest verdrahteter Adapter-Prüfung**:
  `Checker.Register(name, Reporter)` nimmt beliebige Typen mit
  `Healthy() (bool, string)` — MQTT- und CAN-Adapter implementieren es
  jeweils über ein natürliches, bereits vorhandenes Fehlersignal (MQTT:
  `circuitBreaker.isOpen()`, neue unexportierte Methode neben `allow`/
  `recordFailure`/`recordSuccess`; CAN: ein neues `lastReadErr`-Feld,
  gesetzt genau dann, wenn der Read-Loop wegen eines echten Socket-Fehlers
  beendet wird — nicht bei einem regulären `Close()`-Shutdown, das bleibt
  "healthy"). **HTTP implementiert `health.Reporter` bewusst nicht**: Es
  gibt keine persistente Verbindung oder vergleichbaren Fehlerzustand
  (jeder Request trägt sein eigenes Timeout, siehe `httpadapter`s
  Doc-Kommentar zum fehlenden Circuit Breaker) — ein nicht-registrierter
  Adapter fehlt schlicht im `"adapters"`-Objekt der Health-Antwort, statt
  fälschlich als "ok" gemeldet zu werden.
- **`Adapter(s) failed → 200`, nie ein Non-200 wegen eines einzelnen
  Adapters**: `Checker.Handler` gibt `200` zurück, sobald `ready`, unabhängig
  vom Zustand einzelner `Reporter` — die AC verlangt das explizit ("gateway
  still serves other adapters").
- **`SetReady(false)` beim Shutdown-Start, nicht erst danach**: Direkt nach
  dem Signal-Empfang, bevor `grpcServer.GracefulStop()` o.ä. läuft — ein
  Readiness-Probe mitten im Drain sieht so `503`, bevor tatsächlich ein
  Listener wegfällt, klassisches Graceful-Shutdown-Muster für
  Load-Balancer-Evakuierung.
- **Package-level Prometheus-Collectors via `promauto`** (`internal/metrics`),
  nicht per Konstruktor durchgereicht: Der Standard-`client_golang`-Weg
  (`promhttp.Handler()` bedient per Default exakt `prometheus.
  DefaultRegisterer`) — jedes Paket, das eine Metrik braucht
  (`metrics.Interceptor`, `heartbeat.Monitor` per Callback,
  `device_service.go`), importiert `internal/metrics` direkt, ohne dass
  `DeviceService`/`Monitor` eine neue Dependency-Injection-Schicht für
  "hat optional Metriken" bräuchten (anders als z. B. `CapabilityRegistry`,
  die per `SetX`-Methode injiziert wird — Metriken sind hier bewusst
  global, wie es für Prometheus-Client-Bibliotheken idiomatisch ist).
- **`udal_adapter_errors_total` wird in `device_service.go` inkrementiert,
  nicht in den Adapter-Paketen selbst**: `GetProperty`/`SetProperty`s
  mqtt-/http-/can-Zweige sind die eine Stelle, an der jeder
  Adapter-Aufruf-Fehlschlag ohnehin schon einheitlich beobachtet wird
  (unabhängig vom konkreten Fehler) — Inkrementieren direkt neben dem
  bereits vorhandenen `*StatusError`-Mapping-Aufruf, kein Eingriff in die
  drei Adapter-Pakete nötig.
- **`udal_devices_online` über `heartbeat.WithOnStatusChange`** (neue
  `Option`, `NewMonitor(reg, broker, interval, timeout, opts...)` bleibt
  abwärtskompatibel zum einzigen bestehenden Aufrufer in `main.go`):
  `emit()` ruft den Callback zusätzlich zum bestehenden `broker.Publish`
  auf — exakt an der Stelle, die bereits *jede* Online-/Offline-Transition
  abdeckt (Touch für "online", Sweep für "offline"), passend zur AC
  ("increments on RegisterDevice" — RegisterDevice ruft `presence.Touch`
  bereits seit #42 — "decrements on timeout" — Sweep ist per Definition der
  Timeout-Pfad).
- **AC "`udal_request_duration_seconds` p99 bucket matches load test
  measurements" bewusst nicht abgehakt**: Das würde einen erneuten Lauf von
  #43s Load/Soak-Test speziell gegen dieses Histogramm (`prometheus.
  DefBuckets`) mit Bucket-für-Bucket-Abgleich verlangen — nicht Teil dieses
  Tickets, ehrlich als offen dokumentiert statt fälschlich als erledigt
  markiert (analog zu #28s Umgang mit der `trace_id`/OTEL-AC).

## Nachgezogen aus dem Review (vor PR-Eröffnung)

Ein High-Effort-Multi-Agent-Review (8 Finder-Winkel + 1-Voter-Verifikation
je Kandidat) lief gegen den vollständigen Diff, bevor die PR eröffnet wurde.
Sieben Findings wurden CONFIRMED; vier davon waren echte Korrektheitsfehler
und wurden behoben, die übrigen drei bewusst als Low-Severity/vorhandene
Konvention belassen:

- **`SetReady(true)` rannte vor tatsächlichem Port-Bind, nicht nur vor
  Goroutine-Start** (höchste Schwere, unabhängig von zwei Review-Winkeln
  gefunden): REST-Gateway-, Webhook- und Metrics-Listener banden ihren
  Port jeweils *innerhalb* ihrer eigenen Goroutine (`ListenAndServe[TLS]`),
  ohne dass `main()` auf den erfolgreichen Bind wartete — ein
  Port-Konflikt oder TLS-Fehler landete nur in `log.Error`, `SetReady(true)`
  lief trotzdem. `GET /health` hätte `200 {"status":"ok"}` gemeldet, obwohl
  z. B. das REST-Gateway nie tatsächlich lauschte — exakt das
  False-Positive-Readiness-Szenario, das F-21s "not-ready until every
  listener has started" verhindern soll. Fix: alle drei Listener binden
  jetzt synchron per `net.Listen` (TLS wird per `tls.NewListener`-Wrapper
  auf den rohen `net.Listener` angewendet, nicht mehr über
  `ListenAndServeTLS`), mit `os.Exit(1)` bei Bind-Fehler — exakt das
  Muster, das der gRPC-Listener schon vorher hatte. Verifiziert per
  manuellem Smoke-Test: ein blockierter Webhook-Port lässt den Prozess
  jetzt mit Exit-Code 1 fehlschlagen, statt weiterzulaufen.
- **`udal_devices_online` driftete nach Neustart und bei einer bereits
  dokumentierten Touch/Sweep-Race** (zwei Review-Winkel fanden dasselbe
  unabhängig): Der Gauge wurde nur `Inc()`/`Dec()`t, nie auf einen
  absoluten Wert `Set()`t. (a) Geräte-Online-Status ist bbolt-persistent
  über Neustarts hinweg; ein frisch gestarteter Prozess beginnt bei 0,
  sodass der erste Sweep-getriebene `Dec()` für ein bereits-online
  persistiertes Gerät den Gauge negativ treiben konnte. (b) `Touch`s
  Read-then-Write ist laut eigenem Doc-Kommentar bewusst nicht-atomar
  toleriert ("a rare double transition ... is tolerated") — bislang
  harmlos (nur ein doppeltes Broker-Publish), aber jetzt zusätzlich ein
  doppeltes `Inc()` ohne kompensierendes zweites `Dec()`, permanenter
  Drift. Fix: `WithOnStatusChange`s main.go-Callback zählt bei jeder
  Transition per `reg.List(ListFilter{Online: true})` tatsächlich nach und
  `Set()`t den Gauge auf den exakten Wert, statt inkrementell zu
  Inc()/Dec()en — self-correcting bei jeder Transition, behebt beide
  Symptome an der Wurzel statt am Symptom.
- **`StreamInterceptor` maß die Dauer erst beim Stream-Ende** — für
  `StreamCommands` (ein absichtlich stundenlang offen gehaltener Stream,
  siehe dessen eigener Doc-Kommentar) landet jede Beobachtung im
  `+Inf`-Overflow-Bucket von `prometheus.DefBuckets` (max. 10s) — kein
  brauchbares Signal, aber ein irreführendes. Fix: `RequestDuration` wird
  für Streams gar nicht mehr beobachtet (`record(..., recordDuration=false)`
  für `StreamInterceptor`), `udal_requests_total` zählt weiterhin bei
  Stream-Ende nach finalem Status — ein legitimes Signal für "abgeschlossene
  Streaming-Sessions".
- **`GET /health` akzeptierte jede HTTP-Methode identisch**, anders als
  `logging.LevelHandler` auf demselben Mux (das GET/PUT/POST unterscheidet
  und sonst `405` liefert). Fix: `Checker.Handler()` lehnt jetzt Nicht-GET
  mit `405` + `Allow`-Header ab, konsistent mit dem Sibling-Handler.
- **Bewusst nicht behoben**: (a) Die Adapter-Namen-Strings
  ("mqtt_adapter"/"http_adapter"/"can_adapter") sind an mehreren Stellen
  (Metrics-Labels, `health.Register`-Namen, Log-`component`-Werte) hand-
  getippt statt über eine gemeinsame Konstante — der Verifier bestätigte,
  dass dasselbe Muster (rohe String-Literale für denselben Wert an vielen
  Stellen, z. B. `d.Transport == "mqtt"`) bereits die bestehende, verbreitete
  Konvention in `device_service.go` ist; kein neu eingeführtes Problem, daher
  kein Fix in diesem Ticket-Zuschnitt. (b) `fakeSocket.ReadFrame`s `select`
  über `inbox`/`errCh` hat keine Reihenfolge-Garantie zwischen beiden Kanälen
  — aktuell nutzt kein Test `deliver` und `failWith` auf demselben
  `fakeSocket` ohne dazwischen zu synchronisieren, daher rein latent; ein
  warnender Kommentar an `ReadFrame` wurde ergänzt, kein struktureller Fix.

## Testabdeckung

- **`internal/health`**: `health_test.go` — not-ready → 503, ready ohne
  Adapter → 200 ohne `"adapters"`-Feld, ready mit gesundem/degradiertem
  Adapter (immer 200), `SetReady`-Toggle.
- **`internal/metrics`**: `metrics_test.go` (alle vier Collectors unter dem
  in req42.adoc genannten Namen im `Gather()`-Output), `interceptor_test.go`
  (Unary/Stream, `operation`-Kurzname-Extraktion, Status-Code-Mapping inkl.
  Nicht-`status`-Fehler).
- **Adapter-`Healthy()`**: `TestAdapter_Healthy` in `mqtt/adapter_test.go`
  (Circuit Breaker öffnen → unhealthy) und `can/adapter_test.go` (echter
  Socket-Fehler über eine neue `fakeSocket.failWith(err)`-Testhilfe →
  unhealthy; regulärer `Close()` → weiterhin healthy, eigener Test).
- **`heartbeat.WithOnStatusChange`**: `TestMonitor_OnStatusChange` in
  `monitor_test.go` — Touch (online) und Sweep (offline) lösen den Callback
  in der erwarteten Reihenfolge aus.
- **`device_service.go`-Wiring**: `device_service_metrics_test.go` (neu) —
  ein Adapter-Fehler bei `GetProperty` erhöht
  `udal_adapter_errors_total{adapter=...}` für alle drei Transporte
  (Vorher/Nachher-Delta, nicht exakter Wert, da der Prometheus-Registry
  global und über Testläufe hinweg geteilt ist).
- **Manueller End-to-End-Smoke-Test** (kein automatisierter Test, da er
  einen echten Prozessstart + echten Request braucht): Gateway gestartet,
  `GET /health` → `{"status":"ok"}`; ein echter (unauthentifizierter, daher
  401/`Unauthenticated`) REST-Request gegen `ListDevices` ausgelöst; `GET
  /metrics` zeigt danach `udal_requests_total{operation="ListDevices",
  status="Unauthenticated"} 1` und ein befülltes
  `udal_request_duration_seconds`-Histogramm — bestätigt, dass Interceptor,
  Label-Extraktion und `/metrics`-Handler tatsächlich zusammenspielen, nicht
  nur unit-getestet sind.

`go build ./...`, `go vet ./...`, `gofmt -l .`, `go test -race ./...` und
`golangci-lint run` sind grün.
