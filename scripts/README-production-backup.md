# Production backup identity repair

This is an independent control-plane installation, not an application deploy.
Do not install or run a production backup until the operator separately approves.

## Root cause and field semantics

The installed backup script's JSON here-document hardcoded `candidate_commit=603b217`
and `new_api_image_id=sha256:f92c0580e69a7b467fc065806554ec7b5359f7fe22488d28388766e01049449d`.
These were initial deployment metadata, not a backup-tool version. The production
release manifest evolved independently. Backup success only checked the dump TOC,
encryption, JSON syntax, and hashes, so unchanged stale identity was not detected.

In new backups, the existing fields mean the **running release at backup start**:
`candidate_commit` is the actual image's full OCI revision; `new_api_image_id` is the
container's Docker image configuration ID. `ghcr_repo_digest` is the verified exact
registry reference. These concepts remain separate even when their SHA256 strings
happen to match. The Docker inspection, exact Compose override, effective Compose
image and release manifest must agree. The same container incarnation and metadata
must remain present before publishing the manifest; failures return nonzero.

The additive `running_identity` and `backup_identity_schema_version=1` fields carry
the frozen evidence. `database_recovery_point` binds the dump hash to its UTC start
and completion interval; the unchanged pg_dump mechanism provides its own consistent
MVCC snapshot. The interval is not an exported PostgreSQL snapshot or a PITR WAL LSN.

Other static fields (`postgres_version`, `finance_schema_version`, `terms_hash`,
`privacy_hash`) predate this repair. They remain compatibility baseline metadata,
explicitly listed in `legacy_baseline_metadata_fields`; they are not fresh runtime
attestations. The pinned PostgreSQL image is the existing restore/TOC tool version.

## Compatibility and tests

Keep backup schema 2, all existing artifact names, encrypted bundle layout,
GPG Curve25519/AES256-OCB mechanism, offsite hook and retention policy. Add the
effective Compose override to the existing config archive so recovery can restore
the exact reference. Restore readers that use encryption/artifact names and hashes
continue to read these fields. No new production restore script was found in the
managed backup/bin directory; the previous restore procedure is documented in the
infrastructure acceptance record.

Tests use a strict fake Docker CLI, temporary directories, real tar/SHA256/GPG,
and an ephemeral test-only key. They do not mount a Docker socket or contact any
database. They cover old/new running images, invalid identity, missing override or
OCI revision, drift during dump, distinct digest/config IDs, offsite copies, legacy
restore reader operations and preservation of historical backup bytes. They verify
manifest/bundle reader compatibility, not a new full database restore drill.

Historical manifests, including `20261009T030226Z`, must remain byte-for-byte intact.
Their hashes authenticate their files, but their old static identity metadata is
not authoritative evidence of the running release. Use preserved release-control
archives and audit evidence when assessing historical recovery.

## Next separately approved production operation

1. Use the reviewed commit from this PR. Preserve the installed backup script and
   its SHA256; do not edit it in place. Confirm no backup is in progress in the
   controlled maintenance window.
2. Install both reviewed files together using temporary files and atomic renames:
   `scripts/production-backup.sh` to `/opt/clearapi/backups/bin/clearapi-backup.sh`,
   `scripts/backup-identity.py` to `/opt/clearapi/backups/bin/backup-identity.py`.
   Require root:root and mode 0750 for both; verify the full SHA256 values below.
   Do not modify R4, deploy.env, database, business switches or historical backups.
3. Run the identity helper's read-only `capture` mode and R4 `--verify`. Generate a
   new encrypted backup through the existing service. Confirm the manifest records
   the then-running old revision `fb0d30f59d8c6cc17de3f323eeef71029ca8f26e`, not the
   future candidate. Require all identity assertions and the service exit status.
4. Pull the new encrypted file and manifest offsite; compare VPS/manifest/offsite
   SHA256 and sizes. Verify TOC, archive contents and the existing recovery path.
   Stop on any failure; do not overwrite production with a restore drill.
5. Only after a separate deployment approval, deploy the already accepted immutable
   application candidate with `deploy-production.yml` dispatched at an immutable
   ref resolving to `fc564273ff9192b81b0d3f92c996471b9129783c` (if a pinned tag is
   needed, create it only within the later approved release operation), with
   `expected_commit` equal to that full commit,
   `image_digest=sha256:f2b4ee9c699bc2f1ab897623a11b1d347b84f279a7669dbe2873543cd2858c31`,
   `migration_compatible=true`, and `confirmation=DEPLOY_PRODUCTION`.
   This preserves the workflow's exact-ref check and unchanged R4 wrapper hash.
6. Independently verify the deployed identity, health, rollback image and all frozen
   business/accounting invariants. No commercial traffic is authorized by this plan.

No new application RC is needed: these independent control-plane files are not
copied into the runtime image, and R4/deploy workflow/entrypoint are unchanged.
RC `37873394904` for the pinned application ref remains the candidate. If the
operator instead chooses the new main commit as the application deployment ref,
its different OCI revision requires a new complete RC; do not reuse the old digest
with that new commit.

Installation SHA256 (LF bytes from the reviewed Git blobs):

- `production-backup.sh`: `396d1877335b2a428f8caf432e7ec44f5dc38190975a1121869f75913b12a294`
- `backup-identity.py`: `fd6f4fe5bdb4c125dc286df7e80c71e6bcab2285f713f14e74feef543ae1696b`
