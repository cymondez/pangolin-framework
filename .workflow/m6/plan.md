# M6 release acceptance workflow

- Branch: `codex/m6-release-acceptance`; product stays `0.1.0`.
- Authority: implementation specification v1.3, milestone WP18; formal delivery REL-003/A70, REL-005/A72, ORG-014/A95 plus all earlier requirements.
- Lead (Codex): scope, integration, code review, independent acceptance and evidence audit.
- Initial workers: Copilot `zenlinkos/*`; GLM-5.3 implemented the release gate/drills and Flash handled discovery/the evidence index. At the user's later instruction, remaining review, corrections and website work moved to `gpt-6-luna` with `max` reasoning. The restored Zenlinkos allowance does not interrupt already assigned work. No automatic model fallback or nested delegation.
- Ownership: release gate and negative tests; reproducible clean rebuild/upgrade/recovery/security/performance evidence; cumulative cross-platform integration; package READMEs; documentation site only after acceptance.
- Acceptance: all 95 cases and applicable subtests, three profile rows, real pinned PostgreSQL/NATS plus database corrections, exact 90% overall/critical coverage, full race/static/dependency/license checks on same clean source commit. Save raw results and hashes. No required skips or unproven Verified status.
- Integration: inspect worker diffs, reject fake-success tests or weakened contracts, correct defects, run narrow checks then cumulative Windows/Linux gates. Commit local reviewable source for same-commit evidence; no push, tag, publish or deployment.
- Temporary services: inventory before creation; use unique M6 project; delete owned containers/networks/volumes and unused dedicated images after verification, retain logs.
- Final phase: once runtime/release acceptance passes, update beginner-oriented Docusaurus content, execute examples and website checks, and document actual evidence/limitations.
