#!/bin/sh
# Production secret launcher for the new-api image.
#
# Reads the compose-provided secret files under /run/secrets (host root-owned,
# mode 0600, never committed to Git) and exports them into the process
# environment of the exec'd binary. It never echoes values, never enables
# xtrace and never writes plaintext temp files.
#
# Boundary note: the host root user and the Docker daemon can still read the
# environment of a running process (/proc/<pid>/environ). This launcher raises
# the bar for everyone else; it does not make secrets invisible to root.
#
# Usage (via compose entrypoint override, the image default ENTRYPOINT is
# unchanged): entrypoint: ["/entrypoint-secure.sh", "/new-api"]
set -u

load_secret() {
	secret_name=$1
	secret_file=$2
	if [ -f "$secret_file" ]; then
		secret_value=$(cat "$secret_file")
		secret_value=$(printf '%s' "$secret_value" | tr -d '\r\n')
		if [ -n "$secret_value" ]; then
			export "$secret_name=$secret_value"
		fi
	fi
}

load_secret SQL_DSN /run/secrets/sql_dsn
load_secret SESSION_SECRET /run/secrets/session_secret
load_secret CRYPTO_SECRET /run/secrets/crypto_secret

# CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE is provided through the compose
# environment and points directly at the mounted secret file; the key material
# is never exported as an environment value.

exec "$@"
