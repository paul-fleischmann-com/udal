# Plan: #18 — Python SDK (device + application side)

## Ausgangslage

Die gRPC-API (`DeviceService`, inkl. `StreamCommands` seit #12) ist vollständig
vorhanden; das Go SDK (#12, `code/sdk/go/`) dient als Referenzimplementierung für
req42.adoc §7.3s Operationen-Vertrag. `code/sdk/python/` existierte noch nicht,
CI-Job `python-ci` war bereits vorbereitet (läuft nur bei Änderungen unter
`code/sdk/python/**`), aber mangels Code bislang nie ausgeführt.

## Design-Entscheidungen

- **Codegen ohne buf**: `buf.gen.yaml` hat keinen Python-Plugin-Eintrag, und
  buf's Remote-Plugins (`buf.build/protocolbuffers/python` etc.) brauchen zur
  Generierungszeit Netzwerkzugriff auf buf.build — nicht garantiert verfügbar
  überall, wo dieses SDK gebaut wird. Stattdessen: `grpcio-tools`' eigenständiger
  `python -m grpc_tools.protoc`, mit `googleapis-common-protos` (PyPI) als lokale
  Quelle für `google/api/annotations.proto`/`http.proto` (transitive Abhängigkeit
  der HTTP-Annotationen in `device.proto`, die für die Python-Stubs zwar nicht
  semantisch gebraucht, aber zum Parsen syntaktisch aufgelöst werden müssen).
  Generierte Dateien liegen unter `src/udal/v1/` (`# DO NOT EDIT`, wie
  `code/api/proto/gen/` bei Go) und sind eingecheckt, nicht Teil eines
  CI-Codegen-Schritts — `python-ci` braucht daher kein `buf`/`protoc` installiert.
- **Generierte Stubs unter `udal/v1/`, nicht `udal/_generated/udal/v1/`**: Ein
  erster Versuch verschachtelte die generierten Dateien unter einem eigenen
  `_generated`-Unterpaket. `grpc_tools.protoc`s generierte `*_pb2_grpc.py`-Dateien
  importieren aber immer absolut nach dem Proto-Package-Pfad (`from udal.v1 import
  device_pb2`), unabhängig davon, wo die Datei tatsächlich abgelegt wird — mit der
  `_generated`-Verschachtelung hätte dieser Import fehlgeschlagen, da `udal.v1`
  dann nicht existiert (nur `udal._generated.udal.v1`). Lösung: die generierten
  Stubs liegen direkt als eigenes Unterpaket `udal.v1` neben dem handgeschriebenen
  SDK-Code (`udal.client`, `udal.device`, ...) im selben Top-Level-Package — exakt
  der Pfad, den der generierte Code selbst erwartet.
- **`requires-python = ">=3.12"`**, passend zu req42.adoc §6.1s "Minimum versions:
  ... Python ≥ 3.12" und dem CI-Job (`actions/setup-python@v5`,
  `python-version: "3.12"`) — lokale Entwicklung/Tests liefen in dieser Sandbox auf
  3.11 (einzige verfügbare Version), ohne dass 3.12-spezifische Syntax verwendet
  wurde, die das bricht.
- **Fehlerbehandlung**: `UdalError(code: grpc.StatusCode, message: str)` als
  Exception (nicht `(Value, error)`-Tupel wie Go) — exakt req42.adoc §7.3s
  "Python: raises UdalError(code, message)". `wrap_error` konvertiert
  `grpc.aio.AioRpcError` (und jede andere Exception, Fallback `UNKNOWN`) —
  Python-Äquivalent von `code/sdk/go/errors.go`s `wrapError`.
- **Werte-Typen**: `bool | int | float | str | bytes` — dieselbe Einschränkung wie
  Go (kein `json_val`/strukturierte Werte), aus demselben Grund (die Gateway-eigene
  Property-Storage rundet strukturierte Werte noch nicht sauber). `bool` wird vor
  `int` geprüft (`isinstance(x, bool)` zuerst), da `bool` in Python eine `int`-
  Unterklasse ist — ein naiver `isinstance(x, int)`-Check zuerst hätte `True` still
  als `int_val=1` statt `bool_val=True` kodiert (von `test_value_to_proto_bool_not_
  misencoded_as_int` abgesichert).
- **`Device.run()`-Reconnect**: 1s–30s exponentielles Backoff, identische Logik zu
  Go SDK's `Device.Run` (Health-Schwelle: eine Verbindung, die >5s stand, resettet
  das Backoff auf die Basis, statt es von einem vorherigen Ausfall weiter zu
  eskalieren). `StreamCommands`-Antworten werden per `asyncio.create_task` parallel
  bearbeitet (mehrere gleichzeitige Commands möglich, spiegelt Go's `go
  d.handleCommand(...)`).
- **`mypy --strict`, generierte Stubs ausgenommen**: `udal.v1.*` hat keine
  Typannotationen (protoc generiert keine) — per `[[tool.mypy.overrides]]` mit
  `ignore_errors = true` ausgenommen, exakt wie `golangci-lint` `code/api/proto/gen/`
  für Go ausnimmt. Die zwei Aufrufstellen, die *in* das ungetypte Modul hinein
  aufrufen (`DeviceServiceStub(self._channel)` in `client.py`/`device.py`), brauchen
  trotzdem je ein eigenes `# type: ignore[no-untyped-call]` — die
  Modul-Ausnahme deckt nur Fehler *innerhalb* der generierten Datei selbst ab,
  nicht Aufrufe von außen hinein.
- **Coverage-Ausnahme für generierte Stubs**: analog per `[tool.coverage.run]
  omit`, sonst würde der 80 %-Schwellenwert künstlich durch ungetesteten,
  generierten Code verwässert (erste Messung ohne Ausnahme: 33 % — mit Ausnahme
  auf handgeschriebenem Code: 89 %).
- **Tests gegen einen echten In-Process-`grpc.aio`-Server**, nicht gegen
  Stub-Level-Fakes: Python-generierte Client-Stubs sind (anders als Go-Interfaces)
  nicht ohne Weiteres duck-typebar/austauschbar. `tests/conftest.py`s
  `FakeDeviceService` implementiert `DeviceServiceServicer` direkt und läuft auf
  einem echten (aber lokalen) `grpc.aio.server()` — Python-Äquivalent von Go SDKs
  `fakeDeviceServiceClient`, inkl. desselben "StreamCommands schlägt N-mal fehl,
  dann erfolgreich" Musters für den Reconnect-Test
  (`test_run_reconnects_after_stream_failures`, spiegelt `device_internal_test.go`s
  `TestDevice_RunReconnectsAfterStreamFailures`).

## Testabdeckung

- `test_errors.py`, `test_values.py`, `test_channel.py`: Fehler-Wrapping,
  Werte-Rundtrip (inkl. bool/int-Falle), TLS/insecure-Channel-Aufbau.
- `test_client.py`: `get_property` (Erfolg + `NotFound`→`UdalError`),
  `write_property` (inkl. Ablehnung eines nicht unterstützten Typs vor jedem
  RPC-Aufruf), `send_command` (mit/ohne Ergebnis), `subscribe` (mehrere Events),
  Async-Context-Manager-Protokoll.
- `test_device.py`: Registrierung (neu zugewiesene ID, `ALREADY_EXISTS` behält
  die eigene stabile ID, jeder andere Fehler wird propagiert), `publish_property`,
  Command-Dispatch (registrierter Handler, unregistrierter Handler → Fehler-Result),
  Reconnect-mit-Backoff nach simulierten Stream-Ausfällen.
- **Manueller End-to-End-Smoke-Test** (kein automatisierter Test, da er einen
  echten Gateway-Prozess braucht): Gateway mit `UDAL_DEV_INSECURE=true` gestartet;
  `Device.run()` registriert sich, `publish_property`→`Client.get_property`
  Rundtrip, `Client.write_property`, `Client.send_command` über einen echten
  `on_command`-Handler zugestellt, `Client.subscribe` empfängt ein live
  `write_property`-Event — alle fünf Operationen aus req42.adoc §7.3 gegen einen
  echten Gateway-Prozess verifiziert, nicht nur gegen den In-Process-Test-Server.

`ruff check`/`ruff format --check`/`mypy --strict`/`pytest` (93 % Coverage,
≥ 80 % gefordert)/`pip-audit` sind grün — verifiziert sowohl in der
Entwicklungs- als auch in einer komplett frischen venv (Letzteres speziell, um
den `googleapis-common-protos`-Fix unten tatsächlich end-to-end zu bestätigen,
nicht nur gegen eine venv, die die fehlende Abhängigkeit bereits zufällig
installiert hatte).

## Nachgezogen aus dem Review (vor PR-Eröffnung)

Ein High-Effort-Multi-Agent-Review (8 Finder-Winkel + Verifikation, eine
Instanz — CLAUDE.md-Konventionen — traf ein Sitzungslimit und wurde nicht
erneut gestartet, da jede vorherige Instanz dieses Winkels in dieser Session
bereits leer zurückkam: es existiert kein CLAUDE.md in diesem Repo) lief gegen
den vollständigen Diff. Neun Findings wurden behoben:

- **`googleapis-common-protos` fehlte komplett in `pyproject.toml`s
  `dependencies`** (höchste Schwere — ein Review-Winkel verifizierte das
  empirisch in einer frischen venv): Die generierten `udal.v1`-Stubs
  importieren `google.api.annotations_pb2` (aus `device.proto`s
  `google.api.http`-Optionen) — dieses Modul kommt aus dem
  `googleapis-common-protos`-Paket, nicht aus `grpcio`/`protobuf` selbst.
  Ohne die explizite Dependency schlägt `import udal` (und damit jeder Test,
  jede reale Nutzung) mit `ModuleNotFoundError` fehl — bei einem sauberen
  `pip install -e ".[dev]"`, exakt wie `python-ci` es ausführt. War lokal
  unbemerkt, weil die Entwicklungs-venv das Paket bereits (aus der
  Codegen-Vorbereitung) installiert hatte. Fix: als Dependency ergänzt,
  danach in einer komplett frischen venv verifiziert.
- **`Device.run()` beendete sich dauerhaft statt neu zu verbinden, wenn
  `StreamCommands` sauber (ohne Exception) endete** (drei Review-Winkel
  fanden das unabhängig, mit konkretem Beleg aus dem echten Gateway-Code:
  `device_service.go`s `StreamCommands` gibt `nil` zurück sowohl bei
  Server-Shutdown als auch wenn der Commands-Channel serverseitig geschlossen
  wird): Go SDKs `Run()` behandelt einen sauberen Stream-Abschluss identisch
  zu jedem anderen Verbindungsabbruch — die Schleife läuft nur bei
  Cancellation aus. Die Python-Version hatte stattdessen ein `return` direkt
  nach dem sauberen Ende, das `run()` komplett beendete — ein Gerät, das
  einen sanften Gateway-Neustart erlebt, hätte für immer aufgehört, Commands
  zu verarbeiten, ohne jeden Fehler. Fix: die `return`-Anweisung entfernt,
  sodass ein sauberes Ende dieselbe Reconnect-mit-Backoff-Behandlung
  durchläuft wie ein Fehler; neuer Test
  `test_run_reconnects_after_clean_stream_close` (mit einem entsprechend
  erweiterten `FakeDeviceService`) sichert das ab.
- **`asyncio.create_task(...)` pro eingehendem Command ohne gehaltene
  Referenz** (zwei Review-Winkel unabhängig gefunden): asyncio hält für einen
  laufenden Task nur eine *schwache* Referenz, sobald nichts anderes eine
  starke hält — ohne Referenz kann der Task mitten in der Ausführung
  garbage-collected werden, mit nur einer stillen "Task was destroyed but it
  is pending!"-Meldung auf stderr. Fix: laufende Tasks in einem
  Instanz-Set gehalten, per `add_done_callback` selbst entfernt.
- **`_to_value()` kodierte `None` als den String `"None"` statt als
  JSON-`null`** (ein Review-Winkel fand das, empirisch verifiziert): die
  handgerollte `isinstance`-Kette hatte keinen `None`-Fall. Fix: `_to_value`
  nutzt jetzt `Struct.update()`s eigene, bereits korrekte Werte-Koerzion
  (wickelt den Wert kurz in ein Wegwerf-`Struct` und nimmt das Feld wieder
  heraus) statt der handgerollten Kette — behebt gleichzeitig einen zweiten,
  unabhängig gefundenen Finding (stiller `str()`-Fallback für nicht
  erkannte Typen; `Struct.update()` wirft stattdessen einen klaren
  `ValueError`, der von `_handle_command`s bestehendem `except Exception`
  ohnehin schon sauber als Command-Fehler gemeldet wird).
- **`subscribe()` schloss den zugrundeliegenden gRPC-Stream nicht
  deterministisch, wenn der Aufrufer die Iteration vorzeitig abbricht** (ein
  Review-Winkel fand das): ein `break` aus der `async for`-Schleife des
  Aufrufers wirft `GeneratorExit` an der `yield`-Stelle, aber ohne
  `finally: call.cancel()` hing das Schließen des Streams von
  Refcounting-Timing ab, nicht garantiert prompt — ein auf dem Gateway
  liegen bleibender Subscribe-Stream pro abgebrochenem Abonnement. Fix:
  `finally: call.cancel()` ergänzt (No-Op, falls der Call schon beendet
  ist); neuer Test mit `asyncio.wait_for`-Zeitschranke sichert ab, dass
  `aclose()` nach einem frühen Abbruch prompt zurückkehrt.
- **`value_to_proto` warf für einen int64-Überlauf eine rohe `ValueError`
  statt `TypeError`**, was `write_property`/`publish_property`s
  `except TypeError`-Fänger durchbrach und die dokumentierte
  "jede fehlschlagende Operation wirft `UdalError`"-Garantie verletzte (ein
  Review-Winkel verifizierte das empirisch gegen protobuf 7.35). Fix: expliziter
  int64-Bereichscheck in `value_to_proto`, der eine klare `ValueError` wirft;
  beide Aufrufstellen fangen jetzt `(TypeError, ValueError)`.
- **`PropertyUpdate.timestamp` war timezone-naiv statt UTC-bewusst** (ein
  Review-Winkel verifizierte das empirisch): `Timestamp.ToDatetime()` ohne
  Argument liefert `tzinfo=None`, obwohl der Zeitpunkt UTC ist — ein
  Vergleich gegen eine timezone-aware `datetime` hätte eine `TypeError`
  geworfen. Fix: `ToDatetime(tzinfo=UTC)`.
- **Tote Verzweigung**: `MessageToDict(cmd.params) if cmd.HasField("params")
  else {}` — beide Zweige liefern für ein nie gesetztes `params`-Feld
  dasselbe Ergebnis (`{}`), empirisch verifiziert. Auf
  `MessageToDict(cmd.params)` vereinfacht.
- **Ungenutzte Test-Instrumentierung in `conftest.py`**: `get_property_error`
  wurde nie von einem Test gesetzt (entfernt); `received_device_id_header`
  wurde erfasst, aber nie geprüft (jetzt in
  `test_on_command_dispatches_and_writes_result` als echte Assertion
  genutzt, statt totes Gewicht zu bleiben).

Nicht behoben, bewusst als akzeptierte Design-Entscheidung dokumentiert: die
`ClientConfig`/`DeviceConfig`-Feldüberlappung (spiegelt Go's eigene
`Config`/`ClientConfig`-Aufteilung 1:1) und `_register()`, das bei jedem
Reconnect stets `self._config.device_id` statt der zuletzt vom Gateway
zugewiesenen `self._device_id` sendet (identisches Verhalten zu Go SDKs
eigenem `register()`, also übernommenes, nicht neu eingeführtes Verhalten —
ein Fix hier würde von der bewussten 1:1-Parität mit Go abweichen).
