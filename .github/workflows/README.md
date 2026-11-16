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

**Forgejo:** `.forgejo/workflows/ci.yml` wird nach demselben Schema umgebaut, sobald die GHA-Workflows stabil laufen (Folge-Issue zu #208).
