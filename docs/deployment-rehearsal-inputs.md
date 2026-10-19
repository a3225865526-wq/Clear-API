# Deployment rehearsal evidence inputs

Commercial release requires all 11 gates. A successful workflow run alone does
not establish production readiness or authorize public traffic.

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
`migration_compatible=false` remains mandatory. Missing required real Provider
evidence remains a failed gate for commercial release.

The explicit `controlled` rehearsal stage produces a different proof schema,
`clearapi.controlled-release/v1`. It requires all ten other gates and records
`provider_acceptance=deferred_to_owner_after_deployment` and
`public_traffic=blocked`; it never creates a Provider PASS. The automatic
commercial consumer rejects this proof. A fresh owner dispatch with
`release_stage=controlled`, the exact successful controlled rehearsal and
`DEPLOY_CONTROLLED_PRODUCTION` is required. The authorization job verifies the
same RC artifacts, source CI, evidence identities and release circuit before
requesting production credentials. Root independently binds its target to that
job's exact authorization artifact and the actual production predecessor.

Deploy V2 then verifies its pinned traffic helper and writes a root-owned,
fsynced isolation journal before disabling the Caddy container's restart policy
and stopping it. The Gateway must remain bound to `127.0.0.1:3000`; no TCP/UDP
web listener on 80/443 may remain. Failures preserve isolation. Local health,
Runtime, paired Executor, accounting, backup and migration checks remain
mandatory. Public HTTPS health is explicitly `not_run_ingress_isolated`.
Provider checks must use local access or an SSH tunnel during this phase.

There is no automatic opening operation. The durable journal blocks subsequent
ordinary deployment/restart/reconcile operations. After owner Provider
usage/billing acceptance, opening requires a separately reviewed operation,
fresh system/security checks and explicit owner confirmation. Until then, the
recorded edge restart policy must not be restored. An isolation failure needs
operator recovery review; do not delete the journal or reopen the edge.

These workflow/wrapper changes alter the release contract. The current policy
requires a new reviewed main RC after merge. Preserve old evidence verbatim;
do not rebind its commit, digest, manifest, run or schema identity. Reuse the
authenticated encrypted backup as input when candidate-specific verification
is necessarily refreshed. The pre-deploy official fresh backup remains required.
