# Deployment rehearsal evidence inputs

The existing producer and 11 mandatory gates remain unchanged. A successful
workflow run alone does not establish production readiness.

`CLEARAPI_REHEARSAL_BACKUP_EVIDENCE`, `CLEARAPI_REHEARSAL_ROLLBACK_EVIDENCE`, and
`CLEARAPI_REHEARSAL_PROVIDER_EVIDENCE` are trusted operator inputs (JSON or a
regular file), not cryptographically signed results. Only supply them after
the corresponding real execution has been reviewed. Never put credentials,
session material, or customer records in evidence.

Each input must contain:

- `schema`: `clearapi.rehearsal-evidence/v1`
- `conclusion`: `success`, only for the actual verified scope
- `kind`: respectively `backup`, `rollback`, or `provider`
- `source_commit` and `gateway_digest`: the exact RC identities
- `rc_run_id` and `rc_run_attempt`: integer identities of the exact RC execution
- `manifest_sha256`: SHA256 of the downloaded `gateway-image-release.json` bytes
- `source_schema_sha256` and `target_schema_sha256`: exact Manifest schema hashes
- `target_id`: the reviewed target, matching `CLEARAPI_REHEARSAL_TARGET_ID`

Configure `CLEARAPI_REHEARSAL_TARGET_ID` as a repository variable. Executor
repository/source/digest/binary variables must match the existing approved
constants in `scripts/pg03-release-control.py`. This configuration check does
not prove a new Gateway has connected to the live Executor; the existing
deployment Runtime Gate still verifies the actual pair.

Receipts are bound to the RC run and attempt at both assembly and consumption.
Both ends require a well-formed Manifest source/target schema pair and derive
the migration mode from it; a missing or malformed pair fails closed.

Two modes are supported, and the proof must state the one the RC actually
rehearsed:

- `no_schema_change` — used only when `source_schema_sha256` equals
  `target_schema_sha256`.
- `in_place_startup_migration` — used when they differ. PG03 forbids external
  migration flags (`DEPLOY_MIGRATION_FLAGS` must be empty), so the candidate
  image applies its own additive migrations during startup. This mode is
  accepted only when the Manifest also proves the PG03 schema rehearsal
  contract: `schema_version=pg03-schema-rehearsal-v1`, `startup_passes=2`
  (idempotent across two startups on the same database), and
  `source_commit` equal to the candidate commit. The proof must additionally
  reproduce the Manifest's exact source and target hashes.

A proof that claims `no_schema_change` while the Manifest hashes differ is
rejected, and so is an upgrading Manifest with a proof that omits the
migration block, states the wrong mode, or quotes different hashes.
Successful forward restore alone still cannot authorize this route.
`migration_compatible=false` remains mandatory. Broader real Provider
acceptance may be deferred, but missing required real Provider evidence must
remain a failed gate under the current release contract.
