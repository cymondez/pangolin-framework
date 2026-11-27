# Changelog

All notable changes to **panGOlin Framework** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-27

Initial v0.1.0 release of **panGOlin Framework** (`github.com/pangolin-framework/pangolin`).
See [docs/release-notes/0.1.0.md](docs/release-notes/0.1.0.md) for the full release notes, compatibility boundaries, and verification evidence, and visit the [official documentation site](https://pangolin-framework.github.io/pangolin/).

### Added

- **Kernel, Module DAG & Visibility DI** (`kernel/ambient`, `kernel/module`, `kernel/di`, `kernel/config`, `kernel/runtime`, `kernel/app`):
  - Compile-time typed module graph (`module.Of[T]()`), deterministic topological planning, three-phase configuration, sealed visibility-enforced DI container (`AddSingleton` / `AddTransient` / `AddOperation`, `Export`, explicit `Replace`), layered configuration (`defaults < file < environment < cli`), and graceful Quiesce/Drain shutdown.
  - Immutable Copy-on-Write `kernel/ambient` (`ambient.Var[T]`) context propagation with zero global mutable state.
- **Unit of Work & Neutral Persistence** (`persistence/uow`, `persistence/database`, `persistence/postgres`, `persistence/mysql`, `persistence/sqlite`, `persistence/migration`, `persistence/idempotency`):
  - Strict `UnitOfWorkManager.Within` contract (`Required` / `RequiresNew` / `Detach`, `ReadOnly` / `ReadWrite`, completion hooks, `UOW_COMMIT_UNKNOWN`), PostgreSQL pgx adapter, neutral `persistence/database` port with MySQL and pure-Go SQLite adapters, Goose deployment migrations with global advisory locking, and command idempotency.
- **Application & HTTP Transport** (`application`, `transport/http`, `kernel/errors`):
  - Declarative `application.Runner`, `CommandResult` / `QueryResult` / `AuthorizedCommand`, typed HTTP endpoints (`CommandEndpoint` / `QueryEndpoint`), short-scope `StreamOperation`, and RFC 9457 Problem Details mapping.
- **Security, Multi-Tenancy & Optional Organization Module** (`security/*`, `tenancy`, `modules/organization`):
  - Extensible `identity.Verifier` / `Pipeline`, RS256 JWT/JWKS authentication, fail-closed tenant & neutral resource scope isolation, PostgreSQL `FORCE RLS`, deny-by-default authorization with explicit user deny precedence, and optional `modules/organization` (Enterprise / Organization / Membership governance and `resource_scopes` catalog).
- **Reliable Events, Background Jobs & Optional Realtime** (`events/*`, `platform/jobs`, `modules/realtime`):
  - Local FIFO events, versioned Integration Event envelopes, bounded PostgreSQL Outbox/Inbox with lease fencing and NATS JetStream delivery, persistent background jobs with bounded retry and dead-letter replay, and optional SignalR realtime module (`modules/realtime`).
- **Platform Services & Observability** (`platform/audit`, `platform/settings`, `platform/features`, `platform/cache`, `observability`):
  - Transactional required audit and independent security logs, 5–7 layer hierarchical Settings, permission-bounded Features, scoped Memory Cache, and explicit OpenTelemetry tracing/metrics/JSON logs with `/livez` and `/readyz` probes.
- **CLI & Samples** (`cmd/pangolin`, `examples/basics`, `examples/moduleapp`, `examples/bookstore`, `examples/realtime`):
  - `pangolin` CLI supporting `new` (`tenant-only` / `--with organization` / `--realtime`), `module add --preset service`, `migrate create` / `status`, `doctor [--live]`, `generate openapi`, and three-way safe `upgrade` workflow.
- **Versioned Documentation Site** (`docs/site`):
  - Docusaurus documentation website with local zh/en search, runnable example snippets, and GitHub Pages automated deployment.
