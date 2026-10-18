# PG03 Images Mock Acceptance Harness

This directory contains the deterministic, offline acceptance evidence for the
PG03 Images Contract v1 candidate. Nothing here contacts a real provider, reads
real credentials, or touches a production database.

## What it proves

- `provider_mock_test.go` in the Executor worktree proves the controlled
  single-image admission, the two dispatch boundaries, the result-before-success
  persistence order, duplicate-terminal suppression and restart recovery.
- The Gateway worktree's `service/imageexecution`, `model` and `router` tests
  prove the operator policy presets, the durable idempotency binding, the UNKNOWN
  hold and the owner-scoped status endpoint, on SQLite, MySQL 8.0 and
  PostgreSQL 17.6.

## Reproduce

Executor (Linux, receipt permissions and result storage are Linux-only):

```sh
docker run --rm -e CGO_ENABLED=1 \
  -v "<executor worktree>:/src" -v dsh-gomod:/go/pkg/mod -v dsh-gocache:/root/.cache/go-build \
  -w /src golang:1.26.1-alpine \
  sh -c 'apk add --no-cache gcc musl-dev >/dev/null 2>&1; go test -race -count=1 ./sdk/cliproxy/auth/ -run "^TestPG03Images"'
```

Gateway (three isolated databases):

```sh
docker run --rm --network <isolated-net> \
  -e TEST_IMAGE_DIALECT=postgres \
  -e TEST_IMAGE_DSN="host=<pg> user=<u> password=<p> dbname=<db> port=5432 sslmode=disable" \
  -v "<gateway worktree>:/src" ... golang:1.25.1 \
  sh -c 'go test -count=1 ./model/ -run "^TestImageExecutionDatabase$|^TestImageSourcePolicyDatabase$"'
```

Replace `TEST_IMAGE_DIALECT`/`TEST_IMAGE_DSN` with the MySQL values for the
second engine; omit them entirely for SQLite.

## What it does not prove

Simulated-provider acceptance is not real upstream acceptance. Image model
support, real size/quality acceptance, real usage and real provider cost remain
unverified until a separately authorized live run.
