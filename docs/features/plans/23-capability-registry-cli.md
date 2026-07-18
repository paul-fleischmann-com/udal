# Plan: #23 — Capability Registry CLI (udal schema publish/get/list)

## Ausgangslage

F-13. Der Service selbst (`CapabilityService`: `PublishSchema`/`GetSchema`/
`ListSchemas`) ist seit #22 fertig und gemergt. Dieses Ticket ist nur der
CLI-Client obendrauf — kein neuer Server-Code, keine neue RPC.

## Design-Entscheidungen

- **Neues Modul `code/cli`** (eigenes `go.mod`, Eintrag in `go.work`), statt
  den Befehl in `code/gateway/cmd/` unterzubringen: Jede deploybare Einheit
  im Repo ist bisher ihr eigenes Modul (`gateway`, `sdk/go`, ...) — die CLI
  ist ein eigenständig verteiltes Binary (siehe `deployments/` /
  GoReleaser-Erwähnung in CONTRIBUTING.md), kein Teil des Gateway-Servers.
  Struktur mirror `code/gateway`: `code/cli/cmd/udal/*.go`.
- **Keine neue Abhängigkeit für Subcommands**: `flag.FlagSet` +
  manuelles `os.Args`-Dispatching (`udal schema publish|get|list`), analog
  zu `cmd/gateway/main.go`s eigenem `flag`-Gebrauch. Für drei Subcommands
  lohnt sich keine CLI-Bibliothek (cobra o.ä.) — bausteinsicht selbst nutzt
  cobra, aber das ist ein eigenständiges externes Tool, kein Präzedenzfall
  für dieses Repo.
- **Kein eigener `sdk/go`-Reuse für Dial/Auth**: `dial`/`withAPIKey` in
  `code/sdk/go/dial.go` sind unexported (package-intern). Die CLI
  implementiert ihre eigene, kleine Dial-Helper-Funktion (~30 Zeilen,
  TLS-Server-Verifikation optional via `--ca`, `X-API-Key`-Header via
  `--api-key`) statt die SDK-Sichtbarkeit dafür zu erweitern — die SDK ist
  für Anwendungs-/Geräte-Code gedacht (§7.3), nicht für Admin-Tooling; eine
  CLI-spezifische Erweiterung ihrer öffentlichen API wäre unnötige Kopplung.
- **Auth-Scope bewusst eng**: nur `--api-key` (X-API-Key) + optionales
  Server-TLS via `--ca`/`--insecure`. Kein CLI-seitiges mTLS-Client-Zertifikat
  oder OAuth2/JWT-Flow — keine AC verlangt das, und ein Admin-Operator mit
  API-Key deckt den beschriebenen Use-Case ("An operator publishes...")
  vollständig ab. Folgeticket bei Bedarf.
- **`schema list` sortiert client-seitig nach `PublishedAt` absteigend**:
  `CapabilityService.ListSchemas`/`capability.Registry.List` (#22, bereits
  gemerged) garantieren keine Sortierung (bbolt: Byte-Reihenfolge der Keys;
  Memory: Map-Iteration, beides nicht "newest first"). Da AC3 explizit
  "newest first" für den CLI-Befehl fordert (nicht für die RPC selbst), wird
  in der CLI sortiert statt den bereits gemergten Service aus #22
  anzufassen — hält den Diff dieses Tickets auf die CLI beschränkt.
- **`schema get` gibt `Schema.Raw` pretty-printed aus** (`json.Indent`,
  2 Spaces): AC2 verlangt nur "prints ... as JSON"; Pretty-Print ist für
  interaktive Nutzung lesbarer und bleibt trotzdem valides, maschinell
  reparsbares JSON.
- **`schema publish` gibt den Fehler des Servers unverändert weiter**
  (`status.Convert(err).Message()`, kein CLI-seitiges Nachformulieren) —
  genau das verlangt AC1 ("the same error the gateway API would return").
  Keine lokale Schema-Vorvalidierung in der CLI: das würde die
  Meta-Schema-Logik aus #22 duplizieren und könnte abweichen.

## E2E-Testabdeckung

**Korrektur gegenüber der ursprünglichen Annahme (bufconn):** `bufconn` hätte
einen echten `CapabilityService` im selben Prozess gebraucht — der lebt aber
unter `code/gateway/internal/...`, und Gos `internal/`-Sichtbarkeit
verbietet den Import durch `code/cli` (anderer Modul-/Verzeichnisbaum),
unabhängig von `go.work`. Stattdessen: `integration_test.go`
(`//go:build integration`) baut das echte `cmd/gateway`-Binary per
`go build` (Subprozess), startet es mit `UDAL_DEV_INSECURE=true` + einem
Bootstrap-API-Key auf einem freien Port, und ruft dann die CLI-eigenen
`cmdSchema*`-Funktionen (kein Subprozess für die CLI-Seite nötig, da
gleiches Modul) über eine echte gRPC-Verbindung dagegen auf. Damit ist die
Kette Server-Fehlermeldung (#22s echte Validierungslogik) → CLI-Ausgabe
genau der Bug-Typ, den reine CLI-Unit-Tests mit einem gefakten Client
(`schema_test.go`) nicht gefunden hätten — abgedeckt:

- `schema publish` mit ungültigem Schema gegen den echten Service →
  CLI-Fehlerausgabe enthält die echte Server-Fehlermeldung
- `schema publish` mit gültigem Beispielschema (`schema/examples/
  temperature-sensor.json`), dann `schema get` desselben `name@version` →
  Roundtrip gegen den echten Service
- `schema list` nach dem Publish → der veröffentlichte Name taucht auf
- erneutes `schema publish` desselben `name@version` → echtes
  `ALREADY_EXISTS` vom Service, verbatim in der CLI-Ausgabe

## Doku-Status (nach Implementierung)

- `docs/req42/req42.adoc` F-13: neue "CLI"/"CLI Acceptance Criteria"-Blöcke
  ergänzt (die bereits vorhandenen Service-AC-Checkboxen aus #22 blieben
  unverändert — nicht Teil dieses Tickets)
- `architecture.jsonc`: neue Komponente `sdks.cli` (kind `library`, wie die
  übrigen SDKs) + Relationships `operator → sdks.cli` und
  `sdks.cli → gateway.api.grpc`; automatisch in der `sdks`-View enthalten
  (`sdks.*`-Wildcard) — `bausteinsicht validate` grün
- `docs/arc42/arc42.adoc` §5.1/§5.3: CLI in der SDK-Aufzählung und der
  Level-2-Tabelle ergänzt
- `CHANGELOG.md`: Eintrag unter `[Unreleased]`
- `.github/workflows/ci.yml`: **nicht ursprünglich geplant, aber nötig** —
  `code/cli` ist ein neues `go.work`-Modul, aber `go build ./...` funktioniert
  von Repo-Root aus nicht automatisch über mehrere Module hinweg (Go
  verlangt, dass das Pattern-Präfix exakt eines der `use`-Verzeichnisse
  trifft). Alle vier Go-Jobs (`go-build-test`, `go-lint`, `go-security`,
  `go-integration`) liefen bisher nur gegen `./code/gateway/...` — jetzt
  zusätzlich gegen `./code/cli/...`. Der Path-Filter (`go:`) bekam
  `code/cli/**` und `go.work` dazu.
- **Bewusst nicht angefasst:** `code/sdk/go/**` löst den `go`-Filter zwar
  aus, wird aber in keinem der vier Jobs tatsächlich gebaut/getestet — ein
  vorbestehender, unabhängiger Gap (nicht durch #23 verursacht). `go build
  ./code/gateway/... ./code/cli/... ./code/sdk/go/...` lokal verifiziert:
  baut und testet grün. Nicht in diesem Ticket gefixt, um den Diff auf die
  CLI beschränkt zu halten — Folgeticket bei Bedarf.
