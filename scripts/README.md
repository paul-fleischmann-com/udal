# scripts/

Developer scripts for the UDAL repository. Run all scripts from the **repository root**.

---

## `approve-PR.sh`

Approves a GitHub Pull Request as the `dev-paul-fleischmann` reviewer account and switches back to `paulefl` afterwards.

**Prerequisites:** both accounts must be authenticated via `gh auth` (`gh auth status` should list both).

**Usage:**

```bash
bash scripts/approve-PR.sh <pr-number>
```

**Example:**

```bash
bash scripts/approve-PR.sh 146
```

---

## `add-spdx-headers.py`

Adds or verifies `SPDX-License-Identifier: Apache-2.0` headers in all hand-written source files under `code/` (Go, Rust, Python, TypeScript).

**Usage:**

```bash
# Add missing headers
python3 scripts/add-spdx-headers.py

# Check only — exits with code 1 if any header is missing (used in CI)
python3 scripts/add-spdx-headers.py --check
```
