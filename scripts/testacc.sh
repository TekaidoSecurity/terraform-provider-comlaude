#!/usr/bin/env bash
#
# Runs the acceptance suite against the live Comlaude API, safely.
#
# Credentials come from the environment, or from ~/.config/comlaude/env
# (written by scripts/comlaude-credentials-wizard.sh). That file stores
# values UNQUOTED and they may contain spaces, so it is parsed key=value —
# never sourced.
#
# The suite refuses to run without COMLAUDE_TEST_DOMAIN: acceptance tests
# only ever touch that one domain.

set -euo pipefail

ENV_FILE="${COMLAUDE_ENV_FILE:-$HOME/.config/comlaude/env}"

if [[ -f "$ENV_FILE" ]]; then
  while IFS= read -r line; do
    [[ -z "$line" || "$line" != *=* || "$line" == \#* ]] && continue
    key="${line%%=*}"
    value="${line#*=}"
    # Environment wins over the file; the file only fills gaps.
    if [[ "$key" == COMLAUDE_* && -z "${!key:-}" ]]; then
      export "$key=$value"
    fi
  done < "$ENV_FILE"
fi

for required in COMLAUDE_USERNAME COMLAUDE_PASSWORD COMLAUDE_API_KEY COMLAUDE_TEST_DOMAIN; do
  if [[ -z "${!required:-}" ]]; then
    echo "error: $required is not set (and not found in $ENV_FILE)." >&2
    echo "Acceptance tests refuse to run without it. Run scripts/comlaude-credentials-wizard.sh first." >&2
    exit 1
  fi
done

exec env TF_ACC=1 go test -v -timeout 30m -run 'TestAcc' ./internal/provider/ "$@"
