# ClearAPI shop and referral rewards implementation

Base: main f57a392ae860361dae32f38331c0ba3530ad74e4, 2026-10-10.
Design and implementation approved by the operator in this chat.

1. Extend the existing Option configuration and root permissions with a persisted eight-tier catalog. Preserve the existing external purchase entry, without claiming an unverified delivery denomination. Publish new tiers only after explicit delivery verification and URL validation.
2. Preserve AffCode and InviterId; bind verified registrations on the server, without signup rewards or client-selected inviter IDs. Derive invitation counts from persisted relations. Prevent ordinary profile changes from replacing relations or codes.
3. Store eligible redemption rewards in the existing redemption transaction. Independent payouts atomically credit the inviter, enforce lifetime limits, insert a unique main-database ledger, and mark the reward paid. Retry persisted pending work after restart. Emergency disable pauses work; re-enable alone never resumes it.
4. Expose root configuration, explicit paid/free/test/compensation qualification for unused Legacy codes, audit records, payout summaries, and audited per-record resumption. Display only the current user's safe reward records in the wallet.
5. Verify exact integer calculations, eligibility exclusions, duplicate requests, concurrent caps, fault rollback, persistence recovery, verified registration and existing wallet/Billing behavior. Exercise SQLite, PostgreSQL and MySQL with fresh and representative legacy schemas, including repeated migrations.
6. Perform focused review, then commit and submit a PR only after required checks. Follow protected CI/merge/RC gates. Stop before production deployment and production reward activation.

No dependency installation or upgrade, no lock-file changes, no real code purchases, no provider calls, no production data access or balance changes.
