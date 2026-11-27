# Claude Code Instructions

Follow [AGENTS.md](AGENTS.md) and [.github/copilot-instructions.md](.github/copilot-instructions.md) for all work in this repository.

## Mandatory Local-CI-First Rule

- **Never use GitHub Actions as a trial-and-error loop.** Do not push speculative commits, force-pushes, or re-pushed tags.
- Before any `git push` (and only when explicitly requested by the user), run local CI (which runs on BOTH host and a real case-sensitive Linux ext4 Docker container by default) on the clean commit:
  - General changes: `python scripts/local_ci.py --mode fast`
  - Docs/examples changes: `python scripts/local_ci.py --mode docs`
  - Release/workflow changes: `python scripts/local_ci.py --mode release`
  - Full Linux `-race` + Docker Compose CI mirror: `python scripts/local_ci.py --mode ci`
- Never bypass `.githooks/pre-push` (`--no-verify`, `PANGOLIN_SKIP_PREPUSH=1`, or `--no-docker` stamps are forbidden for AI agents).
- If a CI failure cannot be reproduced locally due to environment restrictions, stop and report clearly instead of pushing guesses.
