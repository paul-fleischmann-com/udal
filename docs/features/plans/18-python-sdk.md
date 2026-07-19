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

`ruff check`/`ruff format --check`/`mypy --strict`/`pytest` (89 % Coverage,
≥ 80 % gefordert)/`pip-audit` sind grün.
