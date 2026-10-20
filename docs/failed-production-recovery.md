# Incident 39347012653: controlled postflight recovery

The candidate Gateway was replaced, local health and Runtime passed, then the
paired Executor probe exited before sending authentication. GNU Wget 1.21.3
checks the `--config` path with `file_exists_p` before `run_wgetrc`; a pipe such
as `/dev/stdin` is rejected. The regular private config fix is exercised with
real GNU Wget, including authentication failure, redirect rejection and cleanup.
The upstream implementation is [GNU Wget main.c](https://raw.githubusercontent.com/mirror/wget/v1.21.3/src/main.c).

The host subsequently rebooted. The configured identity bind source is
`/run/clearapi` on tmpfs, created only during deployment, without a boot creator.
Docker could not restart the same Gateway (exit127, missing bind source).
Recovery now also accepts that exact stopped-container error; other stopped
states, image changes or mounts are rejected. The wrapper retains its dedicated
authorization stdin until the recovery helper consumes it.

## Existing rollback assessment

The previous image is still available and the candidate's original encrypted
backup/restore and old-image rollback proof remain valid. However, the installed
wrapper explicitly forbids automatic PG03 rollback. The database is already at
the candidate schema `3c1c77f3...`, whereas the old release used `578bc6f8...`.
The proved isolated rollback restores the database as well as the old image;
the image-only automatic rollback is not a supported recovery for this incident.
Do not invoke it, restore data, or reopen Caddy under this recovery procedure.

## Approved development scope and production boundary

The owner authorized developing this minimal audited recovery, normal PR/CI,
necessary RC and rehearsal. Installation and execution on production require a
separate explicit owner approval after reporting impact and the fallback below.
No production installation or recovery is implied by merging this change.

The new operation is deliberately bound to the original failed run, Gateway and
Executor container IDs, exact frozen manifests/overlay/isolation hashes,
candidate schema, review counts and encrypted pre-deployment backup. It cannot
recover another incident or change the expected financial baseline.

1. Build the reviewed main RC for the changed recovery assets. Keep the original
   RC39339871064 and controlled rehearsal39340908922 unchanged: they identify the
   currently deployed candidate. The new RC qualifies recovery assets; its
   Gateway image is not deployed by this operation.
2. Dispatch `failed-production-recovery.yml`, `mode=rehearse`, with that assets RC
   and `confirmation=REHEARSE_FAILED_RECOVERY`. This has no production credentials.
   Only successful real transport and recovery safety tests publish the exact
   same-source recovery rehearsal artifact.
3. After explicit production approval, audit-install the wrapper, its updated
   control pin, recovery helper and runtime boot assets. Preserve the previous root assets.
   No sudoers changes are required: the existing wrapper entry remains allowlisted.
4. Owner-dispatch `mode=recover`, the same assets RC, successful recovery
   rehearsal and `confirmation=RECOVER_FAILED_CONTROLLED`. The authorization job
   and root helper independently verify current main, fresh owner dispatch,
   attempt1, production environment policy, same-source CI/RC, exact rehearsal
   artifact and installed source bytes. The original failed run remains failed.
5. Under the existing deployment lock, verify the frozen incident and durable
   ingress hold. If the exact diagnosed Gateway is stopped, first append an
   exclusive start-attempt record, verify the installed boot assets, prepare the
   protected directory using the existing identity helper, start the same exact
   container, reconcile its newly observed process identity and wait for local
   health. Then invoke the wrapper's failed postflight mode. This
   shares the complete existing Runtime, pair, binding, backup-timer, catalog and
   billing postflight. It cannot replace images or release the edge. Starting
   the application does run its existing startup migrations and background workers.
6. Recheck the incident and append a root0600/fsynced, exclusive recovery receipt
   at `failed-recovery-39347012653.json`. Keep the original FAILED manifest,
   attempt, job, target, isolation journal and logs byte-for-byte unchanged.

The receipt states `postflight_recovered_ingress_isolated`; Provider acceptance
remains deferred. It does not claim that the old workflow succeeded or clear its
circuit. Ordinary deployment/restart/reconcile remain blocked by the existing
controlled hold and original FAILED record. No automatic public-opening action
is added.

## Impact, fallback and evidence

Production writes include installing reviewed root/boot assets, recreating the
protected runtime directory and identity, starting the same Gateway when needed,
the existing verification audit, a short-lived private probe config and new
append-only attempt/recovery records. Startup may run AutoMigrate, auth artifact
cleanup, system task/instance updates, finance sync, completion/outbox processing
and route ledger recovery. Existing same-image double-start evidence is reused;
review counts and schema must match before and after. No manual financial write,
image replacement, database restore or Caddy start is performed. Every failed
prerequisite stops without publishing a success receipt; a Gateway started by
this attempt is stopped on subsequent failure. The start-attempt latch prevents
automatic replay even after interruption. The
the isolated edge and original failed evidence remain. Existing root assets can
be restored from the audited installation archive if installation or verification
fails; doing so does not change services or financial data. A completed receipt
must never be deleted or rewritten to conceal an outcome.

Public service is still unavailable while the hold remains. Provider business
acceptance and any customer-traffic release require the owner's subsequent
decision and a separately reviewed opening operation. Restoring the old public
service would need an explicitly approved database-and-image recovery procedure;
this minimal postflight operation does not silently perform that larger change.

Authentication references reviewed: [OWASP Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html),
[Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
and [ASVS 5.0.0](https://owasp.org/projects/asvs). Applicable controls are server-side
owner authorization, replay/expiry rejection, retained audit evidence, no usable
credentials in logs/argv, no redirect/proxy credential forwarding and fresh
Runtime authentication. This is focused validation, not an ASVS certification.
