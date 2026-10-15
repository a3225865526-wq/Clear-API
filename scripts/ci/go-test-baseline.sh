#!/usr/bin/env bash
# Compatibility entrypoint for older callers. Historical failures are no
# longer exempt: preserve the exact command result, including build/panic errors.
set -euo pipefail
[ "$#" -ge 1 ] || { echo "usage: $0 <test command...>" >&2; exit 2; }
exec "$@"
