#!/usr/bin/env bash
#
# GHCR exact-digest pull rehearsal (operator-approved pre-deploy validation).
#
# Proves the exact pull path the production deploy job will use, without
# switching anything:
#   * the SSH principal is clearapi-deploy, whose sudoers allowlist contains
#     exactly this script (plus the deploy wrapper and its hash probes);
#   * the pull credential is the CI job's short-lived GITHUB_TOKEN piped in
#     on stdin -- it is never written to any persistent config;
#   * login happens inside a throwaway DOCKER_CONFIG that is removed on
#     exit, so /root/.docker/config.json is never created;
#   * the image is pulled by exact digest (never a tag) and its OCI
#     revision label must match the expected source commit.
#
# The pulled image stays in the local docker cache as pre-staging for the
# real deploy; no container is replaced, restarted or reconfigured.
set -euo pipefail

DIGEST=""
EXPECTED_COMMIT=""

while [ $# -ge 1 ]; do
  case "$1" in
    --digest)          DIGEST="${2:-}";          shift 2 ;;
    --expected-commit) EXPECTED_COMMIT="${2:-}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

[ -n "$DIGEST" ] || { echo "--digest is required" >&2; exit 2; }
[ -n "$EXPECTED_COMMIT" ] || { echo "--expected-commit is required" >&2; exit 2; }
echo "$DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' \
  || { echo "--digest must be exactly sha256:<64 hex>" >&2; exit 2; }
echo "$EXPECTED_COMMIT" | grep -Eq '^[0-9a-f]{40}$' \
  || { echo "--expected-commit must be a full 40-hex commit" >&2; exit 2; }

IMAGE_BASE="${DEPLOY_IMAGE_BASE:-ghcr.io/a3225865526-wq/clearapi}"
GHCR_USER="${DEPLOY_GHCR_USER:-clearapi-deploy}"
IMAGE_REF="${IMAGE_BASE}@${DIGEST}"

# The credential must arrive on stdin. Refuse interactive misuse loudly.
if [ -t 0 ]; then
  echo "refusing: pull credential must be piped in on stdin" >&2
  exit 2
fi

EPHEMERAL_CONFIG="$(mktemp -d)"
chmod 0700 "$EPHEMERAL_CONFIG"
export DOCKER_CONFIG="$EPHEMERAL_CONFIG"
cleanup() { rm -rf "$EPHEMERAL_CONFIG"; }
trap cleanup EXIT

docker login ghcr.io -u "$GHCR_USER" --password-stdin \
  || { echo "rehearsal: GHCR login failed" >&2; exit 1; }

docker pull --platform linux/amd64 "$IMAGE_REF" \
  || { echo "rehearsal: docker pull ${IMAGE_REF} failed" >&2; exit 1; }

docker logout ghcr.io >/dev/null 2>&1 || true
cleanup
unset DOCKER_CONFIG

# Integrity gate 1: the digest we asked for is the digest we got.
docker image inspect --format '{{index .RepoDigests 0}}' "$IMAGE_REF" | grep -q "@${DIGEST}$" \
  || { echo "rehearsal: RepoDigests does not contain ${DIGEST}" >&2; exit 1; }

# Integrity gate 2: the image's OCI revision label matches the expected commit.
REVISION="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$IMAGE_REF")"
if [ "$REVISION" != "$EXPECTED_COMMIT" ]; then
  echo "rehearsal: OCI revision '${REVISION}' != expected commit '${EXPECTED_COMMIT}'" >&2
  exit 1
fi

IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$IMAGE_REF")"

# Credential hygiene: nothing may persist in the default docker config.
if [ -e /root/.docker/config.json ]; then
  echo "rehearsal: FAIL /root/.docker/config.json exists after run" >&2
  exit 1
fi

echo "rehearsal PASS"
echo "  image ref:     ${IMAGE_REF}"
echo "  image id:      ${IMAGE_ID}"
echo "  OCI revision:  ${REVISION}"
echo "  docker config: ephemeral (removed); /root/.docker/config.json absent"
