#!/usr/bin/env bash
set -euo pipefail

PR=${1:?Usage: approve-PR.sh <pr-number>}

gh auth switch --user dev-paul-fleischmann
gh pr review "$PR" --repo paul-fleischmann-com/udal --approve \
  --body "Alle Findings adressiert, Re-Review grün. LGTM 🚀"
gh auth switch --user paulefl
