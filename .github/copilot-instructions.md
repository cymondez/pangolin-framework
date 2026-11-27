# Repository Instructions for GitHub Copilot

Read [AGENTS.md](../AGENTS.md) before making changes. The rules below are **mandatory and non-negotiable**.

## Hard Rule: Verify CI Locally Before Any Push (Never Use GitHub Actions as a Trial-and-Error Loop)

- **NEVER** use GitHub Actions as a debugging or trial-and-error loop.
- **NEVER** push speculative commits, repeated fixup commits, force-pushes, or delete/re-push tags to "see if CI passes". Excessive pushes and CI runs risk GitHub account rate-limiting or suspension.
- **NEVER** run `git push`, `git push --force`, or push/delete tags unless the user explicitly instructs you to push **and** local CI has already passed on the exact clean `HEAD` commit.
- **NEVER** bypass `.githooks/pre-push` with `--no-verify` or `PANGOLIN_SKIP_PREPUSH=1`.

## Required Local CI Commands (Docker Linux ext4 By Default — Run Before Pushing)

Run from the repository root (`python` on Windows, `python3` on Linux/macOS). All modes run inside a real case-sensitive Linux ext4 Docker container (copied into `/work` with Linux named volumes) so Windows NTFS case-insensitivity cannot mask Linux failures:

1. **Default / code / test changes** (host portability + Linux ext4 Docker):
   ```bash
   python scripts/local_ci.py --mode fast
   ```
2. **Documentation (`docs/site/`, `examples/basics/`, `examples/moduleapp/`) changes** (Linux ext4 Docker):
   ```bash
   python scripts/local_ci.py --mode docs
   ```
3. **Workflow / release changes or before tagging `v*.*.*`** (host + Linux ext4 Docker):
   ```bash
   python scripts/local_ci.py --mode release
   ```
4. **Changes touching DB/Broker/coverage/race or `.github/workflows/ci.yml`** (full Docker Compose + pinned Linux/amd64 `-race` + coverage ≥ 90% + drills + SignalR):
   ```bash
   python scripts/local_ci.py --mode ci
   ```

Do **not** pass `--no-docker` to satisfy pre-push verification; `.githooks/pre-push` requires `linux_docker: true` in `.cache/local-ci/<HEAD-sha>.json`.

## Handling CI Failures

1. Read the full failure log first.
2. Reproduce and verify the fix **locally** using the matching `scripts/local_ci.py` mode above.
3. If local reproduction is blocked by missing tools/services (e.g., Docker unavailable), **stop and report the blocker to the user** instead of pushing unverified fixes.

## Key Repository Conventions

- Core framework packages live at the module root (`kernel`, `application`, `transport`, `persistence`, `security`, `tenancy`, `events`, `platform`, `observability`); optional modules live under `modules/`.
- Do not import `examples/` from production packages, and do not bypass `persistence/uow` or PostgreSQL `FORCE RLS`.
- `go.mod` dependencies and licenses are strictly locked in `deploy/dependencies.json`; check `python scripts/contracts.py check` after any dependency or API change.
- Public API surface is frozen in `tests/contract/api.json`; never alter public APIs silently.
