# Project Conventions

## Commit Messages

Every commit message follows this format:

```
#<issue-id> - <type> - <short description>

[optional body]
```

**Examples:**
```
#105 - feat - add README with quickstart and SDK snippets
#42  - fix  - handle nil pointer when device has no schema ref
#78  - docs - document SBOM generation in CONTRIBUTING
     - chore - update Go toolchain to 1.25
```

**Types:** `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `ci`

If there is no associated issue, omit the `#<issue-id> -` prefix.

## Branch Names

```
feat/<issue>-<short-desc>    # new features
fix/<issue>-<short-desc>     # bug fixes
chore/<desc>                 # maintenance, tooling
docs/<desc>                  # documentation only
release/v<x.y.z>             # release preparation
```

Example: `feat/12-go-sdk-device-registration`

## SPDX Headers

Every hand-written source file must carry an SPDX header on its first two lines.
Run `python3 scripts/add-spdx-headers.py --check` to verify, or `python3 scripts/add-spdx-headers.py` to add missing headers.
See [CONTRIBUTING.md](CONTRIBUTING.md#license--spdx-headers) for details.
