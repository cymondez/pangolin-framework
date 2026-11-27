# Design correction integration tests

This package contains isolated PostgreSQL regressions for the 2026-11-25 design corrections. Tests use the `integration` build tag and require the dedicated M6-style environment variables `PANGOLIN_M1_ADMIN_DSN`, `PANGOLIN_M1_APP_DSN`, and `PANGOLIN_M1_WORKER_DSN`.

Each test creates a uniquely named temporary database, applies only its required framework migrations, and drops that database with `WITH (FORCE)` during cleanup. Assertions that prove rollback or durable writes use a separate admin pool after the unit of work completes.

Run from the repository root after the correction code is stable:

```powershell
$env:PANGOLIN_M1_ADMIN_DSN = 'postgres://pangolin_bootstrap:local-bootstrap-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_APP_DSN = 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
$env:PANGOLIN_M1_WORKER_DSN = 'postgres://pangolin_worker:local-worker-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
go test -count=1 -tags=integration ./tests/designcorrections
```

The suite is targeted regression evidence. It does not by itself establish a release Gate or Verified status.
