# CI-Struktur (Variante D, #208)

`ci.yml` ist der **Orchestrator**: erkennt geaenderte Pfade (`dorny/paths-filter`) und ruft pro Gruppe einen wiederverwendbaren Workflow (`workflow_call`) auf. Der Job `CI Gate (all checks passed)` aggregiert alle Gruppen und ist der **einzige Required Check**.

| Workflow | Jobs |
|---|---|
| `ci-go.yml` | build-test, lint, security, release-dry-run, integration (MQTT, CAN) |
| `ci-rust.yml` | rust-ci, rust-embedded-size-check |
| `ci-python.yml` | python-ci |
| `ci-typescript.yml` | typescript-ci |
| `ci-reflex.yml` | reflex-ci |
| `ci-proto-schema.yml` | proto-ci, schema-validate |
| `ci-deploy.yml` | docker-build, demo-compose, helm-lint |
| `ci-repo.yml` | spdx-check, architecture-validate |
| `ci-nightly.yml` | Load/Soak-Test (schedule + manuell) |

`docs.yml` und `release.yml` sind unveraendert eigenstaendig.

Gemeinsame Bausteine: Composite Actions in `.github/actions/` (`setup-go`, `install-syft`).

**Neue Gruppe:** `ci-<gruppe>.yml` mit `on: workflow_call` anlegen, Filter in `ci.yml` (`changes`) ergaenzen, Aufruf-Job hinzufuegen und im `ci-gate` unter `needs` eintragen.

**Forgejo:** `.forgejo/workflows/ci-<gruppe>.yml` spiegelt dieselben Gruppen (gleiche Dateinamen, gleiche Jobs). Forgejo-Actions kennt kein `workflow_call`; daher sind die Forgejo-Workflows **eigenstaendig** (eigener `push`/`pull_request`-Trigger, kein Orchestrator, kein `CI Gate`). Der Pfadfilter steht **nicht** im Trigger, sondern im Job `changes` (Composite Action `.github/actions/changed-paths`); nicht betroffene Jobs werden per `if` uebersprungen. So startet jeder Workflow immer und Required Checks bleiben nie "pending". Required Checks werden in Forgejo pro Job gesetzt (Format `<Workflow> / <Job> (<Event>)`). Abweichungen zu GHA: `upload-artifact@v3` (nicht blockierend), kein `type=gha`-Cache, `buf breaking` gegen lokales `origin/main`, SPDX-Auto-Commit auf `main`-Push. Aenderungen an einer Gruppe immer in beiden Verzeichnissen nachziehen.
