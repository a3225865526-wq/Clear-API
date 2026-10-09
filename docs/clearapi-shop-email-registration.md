# CLEARAPI SHOP + EMAIL REGISTRATION DEVELOPMENT RESULT

Date: 2026-10-09 (Asia/Shanghai)

## 1. Reuse and design

The current ClearAPI main was independently read from GitHub as `0067558c6c835238debae6a1285a3cea6f4c939a`. The latest successful Deploy Production run (`37900835769`) records the same SHA. A fresh public `/api/status` read confirmed username registration and password login enabled, email verification disabled. Public status does not expose a runtime OCI revision; no live production-container revision claim is made.

Reuse the existing PublicLayout, Button, form, password input, theme tokens, i18n, wallet redemption hook and original Legacy endpoint. Existing StoreProductsCard depends on Store enablement and CNY denomination data; it cannot faithfully represent the approved quota card while Store remains off. The new small display catalog avoids that dependency. Products are reviewed static configuration in `web/src/features/shop/catalog.ts`; no product database or order service is added.

Reuse SMTP configuration, SendEmail, the existing in-memory verification cache, IP limiter, EmailVerificationEnabled, password hashing, normalized email queries and existing transactional email writer locks. No second verification system, Redis dependency, schema migration or user-identity migration is introduced.

## 2. Shop

Public route: `/shop`. Navigation: Buy credits / 购买额度. Official product: ClearAPI API credit recharge code at `https://catfk.com/item/qmcse2`.

Display the confirmed 500000 Quota = $1 nominal platform credit rule and explicitly state that the credit is not withdrawable dollars. Show “View price and buy”, with pricing and availability determined by Catfk. Payment does not automatically credit the wallet. Instructions require getting a code and selecting Legacy redemption in the wallet.

The catalog allows HTTPS on exact host `catfk.com`, an alphanumeric `/item/ID` path, no credentials, nonstandard port, query or fragment. Malformed targets disable buying. Links use `noopener noreferrer`. No user-provided redirect parameters are accepted. Purchase navigation does not call any payment, order or redemption API.

Wallet has a shop link and a button that opens/focuses the existing Legacy redemption form. Offline and legacy redemption remain available through their existing endpoints. Payment and Store settings are never changed.

## 3. Email registration and compatibility

With EmailVerificationEnabled off, retain the existing username/password registration UI and backend. With it on, show email/password/code only and generate a cryptographic internal username (`u_` plus 18 UUID hex characters) guarded by the existing unique username index. Never expose the email as display name or mutate an old user.

Codes use secure six-digit randomness, hash-only process memory storage, ten-minute expiry, five failed attempts, existing IP throttling and an additional normalized-mailbox sixty-second cooldown. The bounded cache refuses new codes when capacity is exhausted. SMTP failures invalidate the challenge and emit a generic log event. Delivery responses remain uniform for taken email addresses and failures; the frontend says “If this email is available, check your inbox”, avoiding a false guarantee of delivery.

Account creation runs while the existing proof lock is held. Consume the challenge only after creation commits; preserve original expiry on database failure. Existing transactional email locks guard duplicates. Require server verification on the original registration endpoint. When forced email verification is active, reject new OAuth and WeChat account creation while preserving linked-account login.

Accounts use the common-user role, default group and zero initial credit. Reject verified registration if QuotaForNewUser is nonzero, ignore referral codes in verified mode, and do not generate the historical default auto-group/unlimited token in this mode. No subscriptions are created.

Login first uses the original username query and database collation; otherwise normalize email and require one unique matching account. Duplicate historical emails fail closed for email login; old username login remains available. Reserve email-shaped legacy usernames at registration so a new email cannot be shadowed by an existing login identity. Do not auto-link accounts, collapse provider aliases or treat code delivery as ownership verification. Existing password/reset mechanisms remain in place.

SMTP uses existing server, port, SSL/STARTTLS, sender, account and token settings. Add bounded connect/session timeouts and TLS >=1.2. Production must use SSL or STARTTLS, valid certificate verification (SMTPInsecureSkipVerify=false), an approved sender and a controlled actual delivery acceptance. No real credentials are recorded here.

The existing cache is process-local. Restart rejects outstanding codes safely. Before activation, verify the production topology supports the current single-instance verification cache; requests spanning independent replicas cannot share these codes. Do not enable verified registration before SMTP acceptance. Roll back the feature by turning EmailVerificationEnabled off; existing registered users and their passwords are unaffected. Keep RegisterEnabled / PasswordRegisterEnabled controlled separately.

## 4. Code scope

Worktree: `D:/AI/worktrees/clearapi-shop-email-registration`
Branch: `codex/clearapi-shop-email-registration`
Base: `0067558c6c835238debae6a1285a3cea6f4c939a`

Only authentication/email, display catalog, navigation, wallet UI, translations and relevant tests are modified. No dependencies or lockfiles are changed. No production configuration/database/container, pricing, billing expression, settlement, reservation, Route Chain, CLIProxyAPI, Executor, account pool, provider routing or formal card data is modified. No official card is redeemed.

Changed files:
- `common/email.go`
- `common/email_test.go`
- `common/verification.go`
- `controller/misc.go`
- `controller/oauth.go`
- `controller/security_account_test.go`
- `controller/user.go`
- `controller/wechat.go`
- `model/user.go`
- `web/src/features/auth/constants.ts`
- `web/src/features/auth/hooks/use-email-verification.ts`
- `web/src/features/auth/sign-up/components/sign-up-form.tsx`
- `web/src/features/wallet/components/other-redeem-methods-card.tsx`
- `web/src/features/wallet/index.tsx`
- `web/src/hooks/__tests__/top-nav-links.test.tsx`
- `web/src/hooks/use-top-nav-links.ts`
- `web/src/i18n/locales/en.json`
- `web/src/i18n/locales/zh.json`
- `web/src/routeTree.gen.ts`
- `web/src/features/shop/catalog.ts`
- `web/src/features/shop/index.tsx`
- `web/src/features/shop/__tests__/catalog.test.tsx`
- `web/src/routes/shop/index.tsx`
- `web/src/features/wallet/components/credit-purchase-card.tsx`
- `web/src/features/wallet/components/__tests__/credit-purchase.test.tsx`
- `web/src/features/auth/sign-up/__tests__/registration.test.tsx`

## 5. Verification

- Initial new UI suites: 20 passing tests in catalog, registration, wallet purchase and navigation.
- Final related UI regression: 23 passing tests across registration, wallet purchase, existing Store purchase/redeem and login verification. Related lint: no errors or warnings after correction.
- Frontend production build: passed using the preinstalled dependencies. No local dependency installation or upgrade was performed; the existing node_modules is reused by junction, so frozen dependency verification belongs to CI.
- Native TypeScript check: reports only unchanged `web/src/features/upstream-pool/components/release-controls.test.tsx:3` TS6133 (unused fireEvent). Verified the import exists in the starting main. Not fixed because account-pool code is explicitly out of scope. No new type errors reported.
- Backend build of the root module: passed before the full-test step.
- Focused controller/common tests with `-race`, including SMTP, expiry/replay, failed-transaction proof retention, attempts/cooldowns, concurrent registration, historical duplicates, email-shaped usernames, OAuth/WeChat bypass, old-mode registration and credential-free application logs: passed.
- Real database matrix: SQLite 3.50.4, MySQL 8.4.11 and PostgreSQL 17.11. Final related registration/login tests passed on isolated synthetic databases. No schema change needs migration. Production databases were never used.
- Existing Redemption/StoreRedeem regressions: passed locally. No formal code or live order was used.
- Full local Go runner: environment-blocked by a missing pre-existing json-iterator module in the offline cache, then intentionally stopped. This is not a passing full-suite result. Existing CI remains authoritative for frozen/full dependencies.
- Full local frontend suite: 200/202 files passed; 2126 tests passed and 160 failed, concentrated in unchanged lobe-icon and usage-logs/model-badge suites (missing provider SVG/image rendering). Two representative Claude/Perplexity failures reproduced on a separate untouched checkout of starting main with the same installed dependencies. This confirms a local baseline icon issue; it does not individually baseline-test every failing assertion. Frozen-dependency CI determines the full-suite integration outcome.
- Browser QA: desktop and 390x844 mobile shop; dark theme; mobile single column; no horizontal overflow (scrollWidth=clientWidth=390). Mocked-status registration at 390x844 shows exactly email/password/code plus sign-in. No actual account or purchase was created through the browser.

Security review used [OWASP Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html), [Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) and [ASVS 5.0.0](https://owasp.org/projects/asvs). Applicable focus: server enforcement, uniform responses, password hash preservation, identity ambiguity, replay/expiry/throttling and secret-free logs. This is a scoped regression review, not a certification of whole-application ASVS compliance. Actual SMTP delivery remains unverified.

## 6. Integration

Implementation commit: `0a56e830e5c38b9984edd9dbbe72ed5f48d53058`. PR: [#7](https://github.com/a3225865526-wq/Clear-API/pull/7). Initial CI: [37934063485](https://github.com/a3225865526-wq/Clear-API/actions/runs/37934063485), running when this report was written. Merge, post-merge CI, RC, exact digest and OCI revision remain pending; the final development result will record their actual outcomes. Standard server-side merge controls must be honored without admin bypass. GitHub protection/rules API returned 403 explaining this private repository's account does not support the feature; no protection changes are made.

## 7. Operator information still needed before activation

Approved SMTP server/port and TLS mode, sender address/name, SMTP account and authorization credential, controlled recipient/delivery acceptance, confirmation of verification-cache topology and final email-registration activation approval. Optional confirmed CNY presentation price. The official Catfk product URL is already confirmed and need not be supplied again.

## 8. Current state

DEVELOPMENT verification in progress
RC not started
PRODUCTION DEPLOY NOT AUTHORIZED

Code release and feature activation are separate. Do not deploy production or enable email verification automatically. Stop at the approved release-candidate outcome.
