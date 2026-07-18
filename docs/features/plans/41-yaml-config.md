# Plan: #41 — YAML config file support (gateway.yaml)

## Ausgangslage

Der Gateway liest aktuell ausschließlich flache Umgebungsvariablen
(`UDAL_GRPC_ADDR`, `UDAL_HTTP_ADDR`, `UDAL_REGISTRY_PATH`,
`UDAL_TLS_CERT`/`UDAL_TLS_KEY`, `UDAL_MTLS_CA_CERT`/`UDAL_MTLS_REQUIRED`,
`UDAL_MQTT_BROKER`, etc. — via den `envOr`-Helper in `main.go`). req42.adoc
§7.2 dokumentiert eine YAML-Konfigurationsdatei (`gateway.yaml`) mit
vollständigem Override über `UDAL_`-präfixierte Env-Vars; diese Datei/dieser
Loader existiert noch nicht.

## Env-Var-Namensschema

req42.adocs Beispiel kommentiert nur 3 der ca. 15 Schlüssel explizit mit
ihrem Override-Namen (`grpc_port` → `UDAL_GRPC_PORT`, `http_port` →
`UDAL_HTTP_PORT`, `metrics_port` → `UDAL_METRICS_PORT`). Für die übrigen
Schlüssel gilt:

- **Bereits existierende Env-Vars werden 1:1 wiederverwendet**, nicht neu
  benannt — das erfüllt direkt die Acceptance-Criteria-Vorgabe "Existing env
  vars ... keep working unchanged" und vermeidet zwei parallele Namen für
  dieselbe Sache: `tls.cert`→`UDAL_TLS_CERT`, `tls.key`→`UDAL_TLS_KEY`,
  `tls.ca`→`UDAL_MTLS_CA_CERT`, `registry.path`→`UDAL_REGISTRY_PATH`,
  `adapters.mqtt.broker`→`UDAL_MQTT_BROKER`, `auth.jwks_url`→`UDAL_JWT_JWKS_URL`.
- **Neue Schlüssel ohne bisheriges Env-Var-Äquivalent** folgen dem in der
  Spec sichtbaren Muster `UDAL_<SCREAMING_SNAKE>`: `auth.api_key_header` →
  `UDAL_API_KEY_HEADER`, `adapters.mqtt.client_id` → `UDAL_MQTT_CLIENT_ID`,
  `adapters.http.poll_interval` → `UDAL_HTTP_POLL_INTERVAL`,
  `adapters.can.interface` → `UDAL_CAN_INTERFACE`, `heartbeat_interval` →
  `UDAL_HEARTBEAT_INTERVAL`, `device_timeout` → `UDAL_DEVICE_TIMEOUT`.

## Scope-Entscheidungen

- **Nur Config-Laden + Override-Mechanik ist Teil dieses Tickets** — nicht
  "mache jeden bestehenden Hardcoded-Wert konfigurierbar über die gesamte
  Codebase hinweg". Konkret bleiben folgende Felder im `Config`-Struct
  ladbar/überschreibbar, aber (noch) nicht an echtes Verhalten angebunden,
  weil deren Verbraucher entweder in einem Folge-Ticket liegen oder noch gar
  nicht existieren:
  - `metrics_port` — kein Metrics-HTTP-Endpoint in der Codebase vorhanden.
  - `auth.api_key_header` — der Header-Name `X-API-Key` ist aktuell an zwei
    Stellen hardcodiert (`internal/auth/interceptor.go`,
    `cmd/gateway/main.go`s grpc-gateway-Header-Matcher); das konfigurierbar
    zu machen wäre eine eigenständige, sicherheitsrelevante Änderung an
    Auth-Code, kein Config-Loading-Thema.
  - `adapters.mqtt.client_id` — der MQTT-Adapter generiert seine Client-ID
    aktuell selbst zufällig (`internal/adapters/mqtt/v5.go`/`v3.go`); das
    global konfigurierbar zu machen bräuchte Änderungen im mqtt-Package
    selbst.
  - `adapters.http.poll_interval`, `adapters.can.interface` — HTTP-/CAN-
    Adapter existieren noch nicht (M2/M3).
  - `heartbeat_interval`, `device_timeout` — Verbraucher ist #42
    (Heartbeat-based device online/offline detection), das direkt im
    Anschluss an dieses Ticket bearbeitet wird.

  Alle diese Felder werden trotzdem korrekt geparst/überschrieben und
  unit-getestet — nur eben (noch) nirgends im laufenden Gateway gelesen.
- **Vorrang-Reihenfolge**: bestehende flache Env-Var (falls gesetzt) >
  YAML-Wert (ggf. durch seinen eigenen dokumentierten Env-Var überschrieben)
  > Hardcoded-Default. Das garantiert AC #3 ("no breaking change") trivial:
  ohne Config-Datei ist der Config-Struct zero-value, die Auflösung fällt
  exakt auf den bisherigen `envOr(...)`-Pfad zurück.
- **Fehlende Config-Datei ist kein Fehler** (AC #4) — an jedem aufgelösten
  Pfad (Default `gateway.yaml` im cwd, `--config`-Flag, oder
  `UDAL_CONFIG_PATH`), nicht nur beim Default-Pfad. Eine vorhandene, aber
  fehlerhafte (nicht parsebare) Datei ist dagegen ein harter Fehler
  (`os.Exit(1)`, konsistent mit den übrigen Init-Fehlern in `main.go`).

## Phasen

### Phase 1 — `internal/config`-Package
- `Config`-Struct passend zu req42.adocs YAML-Schema, inkl. eigenem
  `Duration`-Typ mit `UnmarshalYAML` (yaml.v3 kennt `time.Duration` nicht
  nativ als `"30s"`-String)
- `Load(path string) (*Config, error)`: Datei fehlt → `&Config{}, nil`;
  Datei vorhanden aber kaputt → Fehler
- `(*Config) ApplyEnv() error`: pro Feld dessen dokumentierte Env-Var
- Unit-Tests: YAML-Parsing (voll + teilweise befüllt), fehlende Datei,
  kaputte Datei, jede Env-Var-Override einzeln, Duration-Parsing

### Phase 2 — `main.go`-Wiring
- `--config`-Flag (`flag`-Package) + `UDAL_CONFIG_PATH`, Default
  `gateway.yaml`
- Für jeden tatsächlich konsumierten Wert (grpc/http Addr, TLS
  cert/key/ca, registry path, mqtt broker): Auflösung nach obiger
  Vorrang-Reihenfolge über eine kleine, pure, testbare Hilfsfunktion im
  `config`-Package (keine `main_test.go` nötig)

### Phase 3 — Tests
- Unit-Tests für die Auflösungs-Hilfsfunktion (Phase 2)
- Manuelle Verifikation: Gateway mit einer echten `gateway.yaml` starten,
  Override einzelner Werte per Env-Var gegenprüfen, Start ohne jede Datei
  gegenprüfen (bisheriges Verhalten unverändert)

### Phase 4 — Doku + Changelog
