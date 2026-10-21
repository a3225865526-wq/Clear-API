# PG03 candidate qualification protocol — 2026-10-21

**Local implementation/tests PASS; actual candidate qualification BLOCKED; production authorization BLOCKED; deployment NOT RUN.** No approval policy was issued, no production fact was collected, and no production entry was installed or enabled.

## Scope and trust boundary

`scripts/pg03-candidate-qualification.py` exposes `qualify(get_json, get_bytes, policy_provider, request, now=None)`. The request is the existing `clearapi.successor-transaction-request/v1`; its complete canonical bytes, transaction nonce, candidate and control identity are bound to policy. All transports and the clock are trusted caller implementations. GET transports must enforce authentication, GitHub origin, redirect credential handling, response limits, timeouts and cancellation. The module supplies no transport, policy producer, deployment CLI or production access.

The policy provider is an independent trust boundary. On **every** invocation it must authenticate the current owner's server-side approval and live revocation/consumption state. Request JSON, a repository file, a local snapshot, an environment toggle or `approved_by` text alone cannot provide that authority. The coordinator must supply the provider from trusted installation code. Any provider/transport failure propagates as failure; it is never interpreted as PASS. Real approval-source installation and review remain BLOCKED.

The legacy `pg03-release-control.py` `approved_run`/`approved_rc` current-main rules are unchanged. Qualification does not call their admission entry or weaken them. It reuses `release_manifest`, `verified_artifact`, schema receipt/DDL validators and the fixed real-rehearsal negative-probe and persisted-migration validators. The new protocol requires explicit `independently_reviewed_source_control_split` approval for its distinct source/control pair.

## Exact policy

Schema: `clearapi.candidate-qualification-policy/v1`; reject missing/extra fields.

| Fields | Meaning |
| --- | --- |
| `policy_id`, `state`, `scope` | 32-hex policy ID, `active`, `candidate_qualification_only` |
| `source_control_policy`, `approved_by`, `approval_ref` | Explicit reviewed split, owner identity, authenticated source reference |
| `repository`, `repository_id` | Exact repository name and numeric GitHub repository identity |
| `issued_at_utc`, `expires_at_utc` | UTC lease at most 300 seconds; bounded by request expiry |
| `request_sha256`, `transaction_id`, `candidate`, `control_commit` | Exact canonical request hash, nonce, existing seven-field candidate and current main control commit |
| `max_evidence_age_seconds` | Positive approved freshness limit, at most seven days; future and stale run timestamps reject |
| `revoked_policy_ids`, `revoked_source_commits`, `revoked_image_digests`, `consumed_transaction_ids` | Current authenticated revocation/consumption inventories; any match rejects |
| `control_files_sha256` | Current reviewed control files, including qualifier, adapter, transaction, plan, schema/release tools and CI/RC/isolation workflows |
| `source_workflow_files_sha256` | Source commit's independently pinned CI and RC workflow bytes |
| `isolation_control_files_sha256` | Exact historical six-file control inventory recorded by real isolation; verified against immutable GitHub contents and artifact |
| `allowed_control_changes`, `source_control_diff_sha256` | Complete exact approved source-to-control file list and canonical diff projection hash |
| `bindings` | Exact `rc`, `source_ci`, `control_ci`, `isolation` producers |

Each producer binds positive integer `run_id`/`attempt`, exact `source_commit`, `workflow_path` and `event`. RC/isolation additionally bind positive `artifact_id`, exact `artifact_name` and original `archive_sha256`. CI must be push/main. Source CI and RC identify candidate source; control CI identifies live main. Isolation may identify its original independently pinned control commit, preserving the existing valid real rehearsal.

The diff hash is SHA256 of canonical `TX.encoded({'commits': [ordered commit SHAs], 'files': [{'filename', 'status', 'sha'}, ...]})`, using GitHub response order. Source must be the comparison's base and merge-base; control must be its final commit. Truncated/divergent comparisons, duplicate files, renames/removals, unknown paths and unapproved changes reject. Changes to application sources, Dockerfile or dependencies require a new RC and affected acceptance. The bounded control-only paths are scripts, workflows, evidence and the master status; their actual complete differences must equal the approved list, not merely a policy assertion of separation.

## Validation and outputs

Live reads bind repository/owner/ID, current main, strict required checks and enforced protections, both automatic release switches false, active workflow IDs, same-repo/main producer, exact latest run and rerun attempt, all jobs successful, timestamps, original artifact bytes/API digest/size/unexpired status and payload inventory. Current/source/isolation control files are read at immutable commits and SHA256 checked. Source/control, approvals and runs are rechecked after collection. Any expiry, revocation, consumed nonce, version drift, incomplete/UNKNOWN jobs, missing artifact or provenance mismatch rejects.

RC manifest checks bind source/OCI/workflow/CI, exact image digest, image ID, binary hash, platform and deployment binaries. RC target DDL and schema receipt are validated. Isolation checks bind the historical RC archive and both real startup receipts/challenges, exact binary, schema, negative read-only detection and six-container cleanup. No healthy process or schema equality substitutes for migration receipts.

Protocol v1 intentionally supports only the existing reviewed fixed candidate `799a608d1b5a79df0f2ef8031a7885f414125778`, RC `39511197302/1`, digest `sha256:729fdd37591fb333c9cefe202d7c2b7ff4486df31a2689a9b2152abdf7f6965f`, binary `37bb4bac32c86492e9e1bbc78a164ca53c647b15027451861961290020208aa5`. This directly reuses the already reviewed fixed isolation validators; another candidate requires a reviewed protocol version and its own real evidence. No repeated real isolation run was performed for this control-only change.

Output schema `clearapi.candidate-qualification/v1` records `status=PASS`, policy ID/hash, complete request hash/nonce, candidate, current control, manifest hash, producer references, control file hashes and lease. It always records `execution_allowed=false`, `production_authorization=BLOCKED`, `production_acceptance=NOT RUN`. Rechecking the same unconsumed request is allowed for preflight. Cross-request replay or consumed state rejects. A **separate** production grant and durable executor claim under the deployment lock must consume execution authority; qualification is not such a grant.

## Actual local verification

Command run: `wsl -u root --cd /mnt/d/AI/API/worktrees/clearapi-controlled-launch-20261020 python3 scripts/ci/test-pg03-candidate-qualification.py`.

Observed: **13 tests OK, 0 skip, exit 0**. Deterministic GET/approval fixtures cover full success; missing/unreviewed/revoked policy; consumed/cross-request replay; expiry/future evidence; mutable tag/source/RC/binary mismatch; live main/automatic-switch/control-file drift; application-source and incomplete comparison; failed/skipped/UNKNOWN jobs and superseded producer; forged/expired archive and rerun; migration replay/cleanup/schema/control tampering; revocation/rerun during reads, transport loss and duplicate JSON/incomplete ZIP inventory. Tests reuse only the tracked RC archive `evidence/pg03-rc-39511197302/release-original.zip`; isolation is synthesized explicitly as an API fixture and never represented as production or real-run acceptance.

No real policy provider, independent split approval, production grant, fresh production observation or backup/restore evidence was obtained. Therefore this candidate's actual new qualification is **BLOCKED**, regardless of local fixture PASS or previously valid isolated acceptance.
