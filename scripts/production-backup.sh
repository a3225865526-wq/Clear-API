#!/usr/bin/env bash
# ClearAPI production encrypted backup (Stage 2 infrastructure acceptance).
# Root-run; no secret material on stdout/stderr; fail-closed; flock-guarded.
set -euo pipefail

TS="$(date -u +%Y%m%dT%H%M%SZ)"
BASE=/opt/clearapi/backups
STAGE="$BASE/staging/$TS"
ENC="$BASE/encrypted/$TS"
PGIMG=postgres:17.6-alpine@sha256:ef257d85f76e48da1c64832459b59fcaba1a4dac97bf5d7450c77753542eee94
RECIPIENT=D675187C7097EF9D8C3BFF966B1439F896043FFD
KEEP_DAILY=14
KEEP_MONTHLY=6

log() { echo "$(date -u +%FT%TZ) $*" >> /var/log/clearapi/backup.log; }

exec 200>/run/lock/clearapi-backup.lock
if ! flock -n 200; then
  log "SKIP concurrent run detected"
  exit 0
fi

cleanup() {
  local rc=$?
  if [ $rc -ne 0 ]; then log "FAIL backup_id=$TS rc=$rc"; fi
  if [ -d "$STAGE" ]; then
    if ! find "$STAGE" -type f -exec shred -u {} +; then rc=1; fi
    rm -rf "$STAGE"
  fi
  if [ -d "$ENC" ]; then
    if ! chmod 600 "$ENC"/*; then rc=1; fi
  fi
  log "END backup_id=$TS rc=$rc"
  exit $rc
}
trap cleanup EXIT

log "START backup_id=$TS"
mkdir -p "$STAGE" "$ENC" /var/log/clearapi
chmod 700 "$STAGE" "$ENC"

# Freeze the old running release, never the candidate about to be deployed.
IDENTITY_TOOL=/opt/clearapi/backups/bin/backup-identity.py
python3 "$IDENTITY_TOOL" capture > "$STAGE/running-identity.json"
PG03_TOOL=/opt/clearapi/backups/bin/pg03-backup-state.py
PG03_ARTIFACTS=()
if [ -e /opt/clearapi/app/compose-pg03.yaml ] || [ -e /data/clearapi/pool-executor ]; then
  python3 "$PG03_TOOL" capture --root / \
    --archive "$STAGE/clearapi-pg03-$TS.tar.gz" --snapshot "$STAGE/pg03-state-$TS.json"
  PG03_ARTIFACTS=("clearapi-pg03-$TS.tar.gz" "pg03-state-$TS.json")
fi
DUMP_STARTED=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# 1. consistent logical dump (container-local socket; no password in argv)
docker exec clearapi-postgres-1 pg_dump -U clearapi_admin -d clearapi -Fc \
  > "$STAGE/clearapi-pg-dump-$TS.dump"
[ -s "$STAGE/clearapi-pg-dump-$TS.dump" ] || { log "FAIL empty dump"; exit 10; }
DUMP_COMPLETED=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# 2. globals
docker exec clearapi-postgres-1 pg_dumpall -U clearapi_admin --globals-only \
  > "$STAGE/clearapi-pg-globals-$TS.sql"

# 3. config + legal + secrets archives
tar -czf "$STAGE/clearapi-config-$TS.tar.gz" -C / opt/clearapi/app/docker-compose.prod.yml opt/clearapi/app/compose-candidate.yaml
tar -czf "$STAGE/clearapi-legal-$TS.tar.gz" -C / opt/clearapi/legal
tar -cf  "$STAGE/clearapi-secrets-$TS.tar"  -C / opt/clearapi/secrets
# Stage 3: edge configuration (non-secret: Caddyfile + edge compose; ACME keys excluded by design)
tar -czf "$STAGE/clearapi-edge-$TS.tar.gz" -C / opt/clearapi/edge
# Stage 3b: CI/CD deployment control plane (2026-10-08 acceptance). release-manifest
# is the rollback source of truth; the wrapper is root-owned production deploy
# logic; deploy.env holds non-secret deployment parameters only.
DEPLOY_CONTROL=(opt/clearapi/app/release-manifest.json opt/clearapi/bin opt/clearapi/config)
# Preserve the one-use authorization/history; never restore these over live state.
for name in request authorization plan backup state; do
  if [ -f "/opt/clearapi/app/legacy-transition-${name}.json" ]; then
    DEPLOY_CONTROL+=("opt/clearapi/app/legacy-transition-${name}.json")
  fi
done
if [ -f /opt/clearapi/logs/legacy-transition.jsonl ]; then
  DEPLOY_CONTROL+=(opt/clearapi/logs/legacy-transition.jsonl)
fi
tar -czf "$STAGE/clearapi-deployctl-$TS.tar.gz" -C / "${DEPLOY_CONTROL[@]}"
chmod 600 "$STAGE"/*

# 4. TOC parse check of the dump via pinned-image sidecar
docker run --rm -v "$STAGE:/b:ro" "$PGIMG" \
  pg_restore -l "/b/clearapi-pg-dump-$TS.dump" > /dev/null

# 5. bundle + encrypt (AES256 + OCB AEAD; public key only on this host)
if [ "${#PG03_ARTIFACTS[@]}" -gt 0 ]; then
  python3 "$PG03_TOOL" check --root / --snapshot "$STAGE/pg03-state-$TS.json"
fi
tar -czf "$STAGE/bundle-$TS.tar.gz" -C "$STAGE" \
  "clearapi-pg-dump-$TS.dump" "clearapi-pg-globals-$TS.sql" \
  "clearapi-config-$TS.tar.gz" "clearapi-edge-$TS.tar.gz" "clearapi-legal-$TS.tar.gz" "clearapi-secrets-$TS.tar" "clearapi-deployctl-$TS.tar.gz" \
  "${PG03_ARTIFACTS[@]}"
gpg --batch --yes --trust-model always --recipient "$RECIPIENT" --force-ocb \
  --encrypt --output "$ENC/clearapi-backup-$TS.tar.gz.gpg" "$STAGE/bundle-$TS.tar.gz"

# 6. manifest (no secret values)
ENCSHA=$(sha256sum "$ENC/clearapi-backup-$TS.tar.gz.gpg" | awk '{print $1}')
ENCSZ=$(stat -c %s "$ENC/clearapi-backup-$TS.tar.gz.gpg")
MKMD5=$(docker exec clearapi-postgres-1 psql -U clearapi_app -d clearapi -tAc \
  "SELECT md5(value::text) FROM options WHERE key='bootstrap.completion_marker';" | tr -d ' ')
MKSHA=$(docker exec clearapi-postgres-1 psql -U clearapi_app -d clearapi -tAc \
  "SELECT value::text FROM options WHERE key='bootstrap.completion_marker';" | sha256sum | awk '{print $1}')
DUMPSHA=$(sha256sum "$STAGE/clearapi-pg-dump-$TS.dump" | awk '{print $1}')
GLSHA=$(sha256sum "$STAGE/clearapi-pg-globals-$TS.sql" | awk '{print $1}')
CFSHA=$(sha256sum "$STAGE/clearapi-config-$TS.tar.gz" | awk '{print $1}')
LGSHA=$(sha256sum "$STAGE/clearapi-legal-$TS.tar.gz" | awk '{print $1}')
SECSHA=$(sha256sum "$STAGE/clearapi-secrets-$TS.tar" | awk '{print $1}')
EDGSHA=$(sha256sum "$STAGE/clearapi-edge-$TS.tar.gz" | awk '{print $1}')
DPLSHA=$(sha256sum "$STAGE/clearapi-deployctl-$TS.tar.gz" | awk '{print $1}')
BUNSHA=$(sha256sum "$STAGE/bundle-$TS.tar.gz" | awk '{print $1}')
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
HOSTN=$(hostname)
cat > "$STAGE/manifest.json" <<EOF
{
  "backup_schema_version": 2,
  "backup_id": "$TS",
  "created_at_utc": "$NOW",
  "hostname": "$HOSTN",
  "environment": "production-internal",
  "candidate_commit": null,
  "new_api_image_id": null,
  "postgres_image_digest": "sha256:ef257d85f76e48da1c64832459b59fcaba1a4dac97bf5d7450c77753542eee94",
  "postgres_version": "PostgreSQL 17.6",
  "database_name": "clearapi",
  "finance_schema_version": 9,
  "completion_marker_md5": "$MKMD5",
  "completion_marker_sha256": "$MKSHA",
  "terms_hash": "4a9106b6b0221122f9347546b7ee9e996453c172ab2f57b945fdab6f2519ace8",
  "privacy_hash": "137446cfe8a22c53f2dfe92915d4be365310d5823ebc54f433405a8b9224040d",
  "artifacts": {
    "database_dump": {"name": "clearapi-pg-dump-$TS.dump", "format": "pg_dump custom -Fc", "sha256": "$DUMPSHA"},
    "globals": {"name": "clearapi-pg-globals-$TS.sql", "sha256": "$GLSHA"},
    "config_archive": {"name": "clearapi-config-$TS.tar.gz", "sha256": "$CFSHA"},
    "legal_archive": {"name": "clearapi-legal-$TS.tar.gz", "sha256": "$LGSHA"},
    "edge_archive": {"name": "clearapi-edge-$TS.tar.gz", "sha256": "$EDGSHA", "contents": "opt/clearapi/edge (Caddyfile + docker-compose.yml, non-secret)", "acme_private_state": "excluded by design; Caddy re-issues via production ACME on restore"},
    "secrets_archive": {"name": "clearapi-secrets-$TS.tar", "sha256": "$SECSHA", "semantics": "sha256 of plaintext tar input; inside encrypted bundle only"},
    "deploy_control_archive": {"name": "clearapi-deployctl-$TS.tar.gz", "sha256": "$DPLSHA", "contents": "opt/clearapi/app/release-manifest.json + opt/clearapi/bin (root-owned deploy wrapper) + opt/clearapi/config (deploy.env, non-secret parameters) + existing legacy transition proof/journal/audit"}
  },
  "bundle": {"name": "bundle-$TS.tar.gz", "sha256": "$BUNSHA"},
  "encryption": {
    "method": "GnuPG OpenPGP; ECDH Curve25519; AES256 + OCB AEAD (--force-ocb)",
    "key_fingerprint": "$RECIPIENT",
    "key_location": "private key OFFSITE only",
    "encrypted_artifact": {"name": "clearapi-backup-$TS.tar.gz.gpg", "bytes": $ENCSZ, "sha256": "$ENCSHA"}
  }
}
EOF
python3 "$IDENTITY_TOOL" finalize --snapshot "$STAGE/running-identity.json" \
  --manifest "$STAGE/manifest.json" --dump-started "$DUMP_STARTED" --dump-completed "$DUMP_COMPLETED"
if [ "${#PG03_ARTIFACTS[@]}" -gt 0 ]; then
  python3 "$PG03_TOOL" check --root / --snapshot "$STAGE/pg03-state-$TS.json"
  python3 - "$STAGE" "$TS" <<'PYPG03'
import hashlib, json, pathlib, sys
stage, stamp = pathlib.Path(sys.argv[1]), sys.argv[2]
path = stage / 'manifest.json'
manifest = json.loads(path.read_text())
for key, name in [('pg03_state_archive', f'clearapi-pg03-{stamp}.tar.gz'),
                  ('pg03_state_snapshot', f'pg03-state-{stamp}.json')]:
    manifest['artifacts'][key] = {'name': name, 'sha256': hashlib.sha256((stage / name).read_bytes()).hexdigest()}
manifest['pg03_recovery'] = {'schema_version': 1, 'consistency': 'unchanged_across_dump_and_bundle',
    'scope': ['compose-pg03.yaml', 'config/pg03', 'pool-executor/auths',
              'pool-executor/pool', 'pool-executor/receipts'],
    'restore_policy': 'isolated_only; reconcile live runtime high-water marks before production recovery'}
path.write_text(json.dumps(manifest, indent=2) + '\n')
PYPG03
fi
python3 -m json.tool "$STAGE/manifest.json" > /dev/null
mv "$STAGE/manifest.json" "$BASE/manifests/$TS.json"
chmod 640 "$BASE/manifests/$TS.json"

# 7. offsite push hook (disabled until a push-capable destination exists;
#    current offsite is workstation-pull based)
OFFSITE_CONF=/opt/clearapi/backups/bin/offsite.conf
if [ -f "$OFFSITE_CONF" ]; then
  # shellcheck disable=SC1090
  # shellcheck source=/dev/null
  . "$OFFSITE_CONF"
  if [ -n "${OFFSITE_PUSH_CMD:-}" ]; then
    log "OFFSITE push starting"
    bash -c "$OFFSITE_PUSH_CMD" >> /var/log/clearapi/backup.log 2>&1 \
      || { log "FAIL offsite push"; exit 30; }
    log "OFFSITE push ok"
  fi
fi

# 8. retention: keep last 14 daily + last 6 monthly (01-MM), prune only on success
python3 - "$BASE" "$KEEP_DAILY" "$KEEP_MONTHLY" <<'PYEOF'
import os, re, shutil, sys
base, kd, km = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
dirs = sorted(d for d in os.listdir(f"{base}/encrypted") if re.fullmatch(r"\d{8}T\d{6}Z", d))
daily = [d for d in dirs if d[6:8] != "01"]
monthly = [d for d in dirs if d[6:8] == "01"]
victims = set(daily[:-kd]) | set(monthly[:-km])
for v in victims:
    shutil.rmtree(f"{base}/encrypted/{v}", ignore_errors=True)
    mf = f"{base}/manifests/{v}.json"
    if os.path.exists(mf): os.remove(mf)
print(victims if victims else "nothing to prune")
PYEOF

log "OK backup_id=$TS enc_sha=$ENCSHA"
exit 0
