# Orchestration

## Routing
- `P0` uses disjoint read-only agents for core API, CLI/generation, and docs/examples.
- `P1` must complete and pass focused tests before `P2` begins because examples and scaffold consume the new APIs.
- `P2` must complete before `P3` scaffold changes to avoid merging old and new generation patterns.
- `P4` consumes the framework/runtime manifest established by the current scaffold implementation.
- `P5` uses a separate integrity reviewer and validator after implementation agents finish.

## Agent contract
Each implementation prompt includes: objective, owned files, non-owned files, invariants, exact focused validation, and output summary. Workers must preserve concurrent edits, must not commit, and must stop with evidence on failure.

## Integration
Main agent accepts outputs only after reviewing changed files and rerunning focused validation. Overlapping files are never assigned concurrently. Any public API change requires package README/docs or migration note in the same wave.

## Stop conditions
- A focused test fails: stop that packet, diagnose, repair, rerun.
- A worker proposes to weaken visibility, UOW, RLS, outbox, or scope invariants: reject and redesign.
- Upgrade behavior would overwrite user-edited runtime files: reject and require conflict output.
- Validation requiring unavailable external service is recorded as skipped with the smallest exact follow-up command.
