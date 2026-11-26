# 2026-11-25 design correction migration notes

This guide tracks compatibility and operating changes from the D1–D5 design corrections. It is an implementation migration aid, not a release record. The source changes are still under review; do not infer a milestone status, product release, or Verified evidence from this document.

## Neutral data access: explicit write-returning intent

`database.Executor` adds `QueryRowWrite` for a write returning one row and `QueryWrite` for a write returning multiple rows. Both methods run the write guard before sending SQL. Update Repository and adapter/mock implementations that previously used `QueryRow` or `Query` for `INSERT`, `UPDATE`, or `DELETE ... RETURNING`.

This is a Go interface source change for external `database.Executor` implementations. Implement both methods and preserve the guarded row/cursor lifecycle of their read counterparts. `Query` and `QueryRow` remain read methods by caller contract; the API does not classify arbitrary SQL. A Profile that requires the database engine to enforce read-only transactions must reject an Adapter without that capability.

See [the neutral database API migration note](../../persistence/database/MIGRATION.md) and [database package guide](../../persistence/database/README.md) for the method contract and engine matrix.

## Unit of work completion handlers

Manager-controlled Local Event Handler and flush work may use the standard `application.Command` and `application.Query` boundary with `Required` propagation to join the completing root. These operations share its transaction and cannot commit independently. ReadOnly child views remain read-only. `RequiresNew` and ordinary reentry through a captured callback context remain rejected. A failed Required operation poisons the root even when a Handler catches its returned error.

Handlers that need same-transaction work should call the Application Port with the completion context supplied to that Handler. Do not retain a callback context for completion-time reentry or move completion work to another goroutine.

## DI construction and cleanup

Provider Factory and Cleanup callbacks now run outside the Container metadata lock. Singleton creation remains coordinated by provider key; independent keys can resolve while another Factory or Cleanup is slow. Waiters use their own context. Scope and Container Close stop new work, cancel active Factory contexts, and wait only for the configured bound. If a Factory ignores cancellation and later returns an owned resource, Container retains responsibility and performs its cleanup when it becomes available.

Providers must honor their context and cleanup deadline. Keep each external resource under one cleanup owner; do not add a second Module.Stop close for the same Factory-owned resource.

## JWT JWKS cache policy

JWT verification uses a bounded JWKS cache and refresh cooldown instead of fetching the endpoint for every token. The validator has its own bounded fetch timeout (default five seconds), even when the supplied HTTP client's timeout is zero. Configure the TTL and cooldown for the deployment's rotation and availability needs; token verification fails closed when a required refresh cannot establish a current key set. A same-`kid` key replacement is observed at cache refresh, so its visibility is bounded by the configured cache TTL. Unknown-`kid` refreshes are rate-limited; cancellation and failed refresh attempts do not bypass the cooldown. Do not treat the cache as an authorization grant or skip current Tenant, Scope, Permission, or Resource Policy checks.

Malformed JWKS documents, including a missing or null `keys` field, preserve the last valid key snapshot and count as failed refreshes. A syntactically valid document with an explicit empty `keys` array withdraws all keys and causes verification to fail closed until a later valid refresh.

Use a bounded HTTP client and monitor refresh failures, cache age, and issuer availability. The validator does not run a background fetcher. Do not put token values or JWKS response bodies in logs.

## Queue capacity changes

The D5 permit-slot admission mechanism is implemented and passing its regression tests on real PostgreSQL: every bounded INSERT claims a trigger-owned free slot inside the admission statement; the configured-capacity cross-check is supplied per statement as both a transaction-local GUC and an independent integer argument; refusal, unprovisioned, config-conflict, required, and upgrade-required outcomes are typed; rollback frees the reserved slot, terminated backends release their claim, retained terminal rows keep counting toward the cap until Purge deletes the row, duplicate inbox delivery needs no permit, and unbounded stores stay compatible while failing closed against provisioned queues. `events.ProvisionQueues` provisions both queues in ONE transaction and `jobpg.ProvisionQueue` handles single-queue installs; deployment provisioning alone takes locks, adopts existing rows transactionally, refuses targets below live rows, and grows by reusing the raised maximum slot ID without compacting live bindings.

Upgrade steps for existing deployments: apply `4100002`/`4100003`, then run the deployment provisioning (`ProvisionQueues` / `ProvisionQueue` with action `resize`) with the configured capacities following the migration; provisioning adopts legacy rows transactionally. There is no fairness or throughput guarantee implied by this mechanism, and the historical runbook rows remain authoritative until the next cumulative release gate re-records the operations material.

The ten supplemental regressions are tracked as named `A63/permit-*` cases against the same `OPS-002` requirement through the existing M4 `tests/contract/m4.json` mapping format; no existing case ID was renamed or renumbered. Formal Verified acceptance still requires a clean same-source cumulative release Gate.

## Verification boundary

Targeted PostgreSQL and unit regressions are scoped to their recorded source and scenarios. The historical M0–M6 evidence remains bound to its original commits. Do not update milestone status or product version until a clean, same-source cumulative Gate has passed on every required platform and Profile.
