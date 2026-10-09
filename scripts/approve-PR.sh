#!/usr/bin/env bash
set -euo pipefail

PR=${1:?Usage: approve-PR.sh <pr-number>}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CFG="$SCRIPT_DIR/userDefinition.cfg"

if [[ ! -f "$CFG" ]]; then
  echo "Error: $CFG not found. Copy scripts/userDefinition.cfg.template to scripts/userDefinition.cfg and fill in your accounts." >&2
  exit 1
fi

# shellcheck source=userDefinition.cfg.template
source "$CFG"

: "${REVIEWER_ACCOUNT:?REVIEWER_ACCOUNT not set in $CFG}"
: "${MAIN_ACCOUNT:?MAIN_ACCOUNT not set in $CFG}"

gh auth switch --user "$REVIEWER_ACCOUNT"
gh pr review "$PR" --repo paul-fleischmann-com/udal --approve \
  --body "Alle Findings adressiert, Re-Review grün. LGTM 🚀"
gh auth switch --user "$MAIN_ACCOUNT"
