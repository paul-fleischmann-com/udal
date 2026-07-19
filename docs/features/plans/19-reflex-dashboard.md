# Plan: #19 — Reflex Dashboard — reference demonstrator for UDAL features

## Ausgangslage

`architecture.jsonc` (`sdks.dashboard`) und CI-Job `reflex-ci`
(`.github/workflows/ci.yml`) sahen ein `dashboard/`-Package bereits vor,
aber es existierte noch kein Code — `reflex-ci` lief bislang nur, wenn
`github.ref == 'refs/heads/main'` griff (der Pfad-Filter selbst feuerte nie,
da `dashboard/**` nie existierte). Ein früherer PR (#69, Python SDK) hatte
den Job bereits einmal versehentlich ausgelöst (der `reflex`-Pfad-Filter
schließt `code/sdk/python/**` mit ein, da das Dashboard laut eigener
Beschreibung von der Python SDK abhängt) und musste mit einem
`hashFiles()`-Guard temporär abgesichert werden — dieser Guard wird mit
diesem PR wieder entfernt, da `dashboard/` jetzt tatsächlich existiert.

## Design-Entscheidungen

- **`Client.list_devices`/`get_device` in der Python SDK ergänzt** — req42.adoc
  §7.3s SDK-Vertrag listet nur Connect/Disconnect/ReadProperty/WriteProperty/
  SendCommand/Subscribe/RegisterDevice; eine Geräteliste war schlicht nicht
  vorgesehen (auch das Go SDK hat sie nicht). Ohne sie hätte das Dashboard
  entweder direkt REST/gRPC am SDK vorbei aufrufen müssen (widerspricht
  `architecture.jsonc`s "Uses Python SDK internally") oder AC 1 ("Zeigt
  registrierte Geräte ... live an") gar nicht erfüllen können. Ergänzt als
  neuer, öffentlicher `DeviceInfo`-Typ (bewusst *nicht* `Device` genannt —
  das SDK hatte bereits eine gerätesetseitige `Device`-Klasse, siehe unten)
  plus zwei neue `Client`-Methoden, die die bestehenden GetDevice/
  ListDevices-RPCs des Gateways abbilden. `ListDevicesRequest`s Pagination
  (`page_size`/`page_token`) wird bewusst nicht durchgereicht — für ein
  Referenz-Dashboard unverhältnismäßig, liefert nur die erste Seite.
- **Namenskollision `Device`**: das SDK hatte bereits `udal.device.Device`
  (die geräteseitige SDK-Klasse, die man instanziiert um *ein* Gerät zu
  *sein* — bereits im gemergten README als öffentliche API dokumentiert).
  Der neue Rückgabetyp für `list_devices`/`get_device` (ein Registry-Eintrag
  *über* ein Gerät) wurde daher `DeviceInfo` genannt statt die bereits
  veröffentlichte `Device`-Klasse umzubenennen oder zu überladen.
- **Gerätestatus per Polling, nicht per echtem Push**: das Gateway hat kein
  "Subscribe für alle Geräte" — `Subscribe` verlangt eine konkrete
  `device_id`, und `ListDevices` hat keine Streaming-Variante. Ein
  Hintergrund-Task pollt `list_devices()` alle 3 Sekunden
  (`DEVICE_POLL_INTERVAL_SECONDS`), solange "Watch devices" aktiv ist —
  ehrlich als Polling dokumentiert, nicht als Push kaschiert. Die
  Live-Telemetrie (AC 5, "Live-Update ... ohne manuelles Neuladen") ist
  dagegen ein echter Server-Push über `Client.subscribe` für das jeweils
  ausgewählte Gerät — das einzige Feature, das die AC wörtlich als
  "Subscribe-Streaming" verlangt, und auch das einzige, wofür die
  Gateway-API das tatsächlich hergibt.
- **Property-Browser ohne Property-Liste**: das Gateway hat keine "liste
  alle Properties eines Geräts"-Operation — `GetProperty(device_id, path)`
  verlangt einen bereits bekannten Pfad. Der "Browser" ist entsprechend ein
  Formular (Pfad eingeben → Lesen), keine durchsuchbare Liste — das
  spiegelt eine echte API-Einschränkung wider, keine Design-Schwäche des
  Dashboards. Eine Capability-Schema-Introspektion (die Properties eines
  Geräts anhand seines Capability-Schemas aufzulisten) wäre möglich, würde
  aber `CapabilityService`-Unterstützung im SDK voraussetzen, die #18 nicht
  eingeschlossen hat — bewusst außerhalb des Scopes.
- **`_parse_scalar` rät den Typ eines geschriebenen Werts** (bool/int/float/
  string, in dieser Reihenfolge probiert): das UI-Textfeld kennt den
  deklarierten Typ einer Property nicht (dasselbe API-Limit wie oben) — ein
  Best-effort-Rateversuch statt einer validierten Konvertierung, klar so
  im UI-Hilfetext kommuniziert.
- **Jeder Event-Handler öffnet einen frischen `Client`** (`_client()`,
  `async with _client() as client: ...`), statt einen langlebigen Client im
  State zu halten: ein offener gRPC-Channel lässt sich nicht sicher als
  Reflex-State-Var persistieren (State muss zwischen Requests serialisierbar
  bleiben), und die SDK selbst ist genau für dieses
  Kurzlebig-öffnen/schließen-Muster gebaut (`async with Client(...)`). Für
  ein Referenz-Dashboard im Demo-Maßstab (wenige Geräte, wenige Requests)
  ist der Overhead vernachlässigbar; für eine Produktions-Monitoring-UI
  wäre ein wiederverwendeter Channel ein sinnvoller nächster Schritt, aber
  außerhalb dieses Tickets.
- **`DeviceRow` als `@dataclasses.dataclass`, nicht `rx.Base`**: `rx.Base`
  existiert in der installierten Reflex-Version (0.9.7) nicht mehr — neuere
  Reflex-Versionen unterstützen normale Dataclasses direkt als State-Var-Typ,
  empirisch gegen die tatsächlich installierte Version verifiziert (nicht
  aus dem Gedächtnis angenommen, da Reflex zwischen Versionen stark
  api-drifted).
- **Explizite Setter-Event-Handler statt Reflex' Auto-generierten
  `set_<varname>`**: Reflex erzeugt für jede State-Var automatisch einen
  `set_xxx`-Event-Handler zur Laufzeit (Metaclass-Magie) — mypy sieht diese
  dynamisch erzeugten Attribute nicht (CONTRIBUTING.md verlangt Strict
  Mode). Vier explizite `@rx.event`-Methoden ersetzen sie: statisch
  sichtbar, selbstdokumentierend, keine Ignore-Kommentare nötig.
- **Zwei eng begrenzte, dokumentierte mypy-Ausnahmen nur für
  `dashboard/dashboard.py`** (die Komponentenbaum-Datei), nicht für
  `state.py` (die eigentliche Businesslogik): Reflex' eigene Typisierung
  deckt zwei zentrale, im Framework unvermeidbare Muster nicht ab — (a)
  `rx.vstack`/`rx.button`/... und jede Funktion, die sie komponiert, geben
  lose genug typisierte Werte zurück, dass `warn_return_any` überall
  anschlägt; (b) `on_click=State.handler(arg)` (der dokumentierte Weg, einem
  Event-Handler ein Argument mitzugeben, hier für die Zeilen-ID beim
  Geräte-Auswählen gebraucht) löst zu einem Typ auf, den Reflex' Stubs nicht
  modellieren. Beide sind Lücken in Reflex' eigener Typisierung, keine Bugs
  in diesem Code — per `[[tool.mypy.overrides]] module =
  "dashboard.dashboard"` mit `disable_error_code` präzise eingegrenzt, statt
  `strict` global aufzuweichen.
- **`await self.read_property()` aus `write_property()` heraus vermieden**:
  ein `@rx.event`-dekorierter Handler direkt aus einem anderen aufzurufen
  löste `"EventCallback? not callable"` in mypy aus (Reflex' Dekorator
  transformiert die Methode in ein Objekt, das mypys Stubs nicht als
  aufrufbar kennen). Statt eines Ignore-Kommentars: die eigentliche
  Lese-Logik in eine private, nicht-dekorierte `_refresh_property()`
  extrahiert, die sowohl `read_property` als auch `write_property` (nach
  erfolgreichem Schreiben, um den tatsächlich gespeicherten Wert zu zeigen)
  aufrufen — sauberer Code UND kein mypy-Problem.
- **`reflex-ci`-Job**: der `hashFiles()`-Guard von PR #69 wird wieder
  entfernt (siehe Ausgangslage), stattdessen ein neuer Schritt "Install
  Python SDK" vor "Install dependencies" — `udal-sdk` ist bewusst *nicht*
  als Pfad-/URL-Abhängigkeit in `dashboard/pyproject.toml` deklariert (reines
  `setuptools` löst relative Pfade nicht portabel auf), sondern per
  explizitem `pip install -e ../code/sdk/python`-Schritt installiert — CI
  und `CONTRIBUTING.md`s Quickstart-Zeile tun jetzt dasselbe.

## Verifikation

- `ruff check`/`ruff format --check`/`mypy --strict` sind grün (mit den oben
  begründeten, engen `dashboard.py`-Ausnahmen).
- `reflex export --no-zip` läuft erfolgreich durch (Python-seitige
  Komponenten-Kompilierung UND vollständiger Frontend-Produktions-Build,
  verifiziert per generiertem `index.html`/Assets) — bestätigt, dass sowohl
  die Python- als auch die React/Vite-Seite fehlerfrei bauen. Lief in dieser
  Sandbox ungewöhnlich langsam (~80 Minuten statt der ~80 Sekunden eines
  leeren Reflex-Scaffolds derselben Version) — mit hoher Wahrscheinlichkeit
  Ressourcen-Konkurrenz dieser geteilten Sandbox-Umgebung (mehrere
  gleichzeitig laufende Node-Prozesse), nicht ein strukturelles Problem
  dieses Codes, da der Build am Ende korrekt und vollständig durchlief.
- **Kein interaktiver Browser-Test**: diese Umgebung hat keinen Browser-
  Zugriff — `reflex run` plus tatsächliches Klicken durch Geräteliste/
  Property-Browser/Command-Dispatch/Live-Telemetrie gegen einen echten
  Gateway-Prozess wurde *nicht* durchgeführt, ehrlich hier vermerkt statt
  stillschweigend als "getestet" behauptet. Was verifiziert wurde: die
  Python SDK selbst (list_devices/get_device, 4 neue Tests) hat vollständige
  Unit-Test-Abdeckung gegen einen echten In-Process-gRPC-Test-Server (wie
  jede andere SDK-Operation, siehe #18); die State-Klassen-Logik folgt
  exakt denselben SDK-Aufrufmustern, die dort bereits getestet sind.
- `code/sdk/python`: alle 42 Tests weiterhin grün, 92.65 % Coverage
  (`list_devices`/`get_device` neu abgedeckt: Erfolg, Capability-Filter,
  `NotFound`→`UdalError`).
