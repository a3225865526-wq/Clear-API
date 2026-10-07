#!/usr/bin/env bash
#
# CI secret scan: fails when tracked files contain credential material or
# forbidden artifact types. Patterns are deliberately narrow to avoid noise
# from test fixtures; a line-level allow marker exists for vetted exceptions.

set -euo pipefail

fail() { printf '[secret-scan] FAIL: %s\n' "$*" >&2; exit 1; }
warn_hit() { printf '[secret-scan] hit: %s\n' "$*" >&2; }

# --- forbidden file paths ---------------------------------------------------
forbidden_path_regex='(^|/)(\.env(\..*)?|.*\.pem|.*\.p12|.*\.pfx|.*\.keystore|id_rsa.*|.*_rsa|.*\.ppk)$'
allowed_path_regex='^(docs|web|fixtures|e2e|electron)/|test|fixture|example|template|\.sample$'

hits="$(git ls-files | grep -E "$forbidden_path_regex" | grep -Ev "$allowed_path_regex" || true)"
if [ -n "$hits" ]; then
  warn_hit "forbidden file path(s) tracked in git:"
  printf '  %s\n' $hits >&2
  fail "forbidden file paths present"
fi

# --- credential content patterns --------------------------------------------
# PEM-parsing source literals (pem.Decode format strings, string-replace of
# the marker) are excluded by line context: a committed key file's BEGIN line
# has no such code context on the same line and is still caught.
pattern_hits="$(git grep -nIE \
  -e '-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----' \
  -e 'ghp_[A-Za-z0-9]{36,}' \
  -e 'gho_[A-Za-z0-9]{36,}' \
  -e 'github_pat_[A-Za-z0-9_]{22,}' \
  -e 'AKIA[0-9A-Z]{16}' \
  -e 'xox[bpars]-' \
  -e 'sk-ant-api[0-9A-Za-z-]{20,}' \
  -- . 2>/dev/null | grep -Ev "$allowed_path_regex" \
    | grep -Ev 'pem\.Decode|ReplaceAll\(|-----END [A-Z0-9 ]*PRIVATE KEY-----' || true)"

if [ -n "$pattern_hits" ]; then
  warn_hit "credential-looking content found:"
  printf '  %s\n' "$pattern_hits" >&2
  fail "credential content present in tracked files"
fi

printf '[secret-scan] PASS: no secrets or forbidden files in tracked content\n'
