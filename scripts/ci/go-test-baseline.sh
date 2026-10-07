#!/usr/bin/env bash
#
# Baseline-aware Go test runner (PRODUCTION CI/CD ACCEPTANCE spec §11).
#
# Usage: go-test-baseline.sh <test command...>
#   go-test-baseline.sh make test
#   go-test-baseline.sh go test -race -count=1 ./model/...
#
# Runs the command, extracts top-level "--- FAIL:" test names, and fails only
# on failures that are NOT recorded in scripts/ci/test-baseline.txt. Baseline
# hits are printed as explicitly acknowledged. A command that exits non-zero
# without any parsed failure (build error, panic before reporting) still
# fails — the baseline can never mask infrastructure breakage.

set -uo pipefail

BASELINE_FILE="$(dirname "$0")/test-baseline.txt"
[ -f "$BASELINE_FILE" ] || { echo "[baseline] missing $BASELINE_FILE" >&2; exit 2; }
[ $# -ge 1 ] || { echo "usage: $0 <test command...>" >&2; exit 2; }

LOG="$(mktemp)"
trap 'rm -f "$LOG"' EXIT

set +e
"$@" > >(tee "$LOG") 2>&1
RC=${PIPESTATUS[0]}
set -e

FAILURES="$(grep -E '^--- FAIL: ' "$LOG" | sed -E 's/^--- FAIL: ([^/ (]+).*/\1/' | sort -u || true)"
[ -n "$FAILURES" ] || FAILURES_FILE=""
FAILURES_FILE="$(mktemp)"
printf '%s\n' "$FAILURES" | sed '/^$/d' | sort -u > "$FAILURES_FILE"

BASELINE_NAMES="$(mktemp)"
grep -vE '^\s*(#|$)' "$BASELINE_FILE" | awk -F/ '{print $NF}' | sort -u > "$BASELINE_NAMES"

ACK="$(comm -12 "$FAILURES_FILE" "$BASELINE_NAMES")"
NEW="$(comm -23 "$FAILURES_FILE" "$BASELINE_NAMES")"

rm -f "$FAILURES_FILE" "$BASELINE_NAMES"

echo "[baseline] go test exit code: $RC"
if [ -n "$ACK" ]; then
  echo "[baseline] acknowledged known-baseline failures:"
  printf '[baseline]   %s\n' $ACK
fi
if [ -n "$NEW" ]; then
  echo "[baseline] NEW REGRESSIONS (not in baseline):" >&2
  printf '[baseline]   %s\n' $NEW >&2
  echo "[baseline] failing pipeline; fix the code or, for a genuine new baseline," >&2
  echo "[baseline] document it in scripts/ci/test-baseline.txt with evidence" >&2
  exit 1
fi
if [ "$RC" -ne 0 ] && [ -z "$ACK" ]; then
  echo "[baseline] command failed without parsed test failures (build/infra error)" >&2
  exit 1
fi
echo "[baseline] PASS (no new failures beyond the documented baseline)"
