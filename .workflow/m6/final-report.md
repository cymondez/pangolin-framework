# M6 completion report

- Branch: `codex/m6-release-acceptance`.
- Runtime acceptance source: `1d7ef1cf0dfdb7ee7b0211d04ad3380329883cb1`.
- Same clean source passed native Windows and pinned Linux, including full Linux integration race. The release gate was independently revalidated after documentation integration.
- All 95 requirements and 421 named subtests are accounted for across three profiles: 1185 applicable rows and 78 explicitly justified N/A rows. The 13 M6 cases include real clean rebuild, forward upgrade, pg_dump/restore, security and measured load drills.
- Raw production statement coverage: Windows 5393/5789; Linux 5392/5789. Overall and each critical package exceed 90%; no required skipped cases.
- Review resolved evidence provenance, malformed/nonfinite metric rejection, coverage raw counts, PostgreSQL client selection, fresh database grants, readiness retry, deterministic queries and distinct JWT issuer/token transport endpoints. Historical evidence was not relabeled.
- Each changed package/tool area has its README or scoped guide. Durable acceptance summary and machine-readable index are in `docs/evidence/M6.md` and `docs/evidence/m6-1d7ef1cf0dfd.json`.
- Docusaurus was updated only after runtime acceptance. Ten standalone consumer examples, real PostgreSQL backup/restore guide execution, npm tests, Windows/Linux production build and browser navigation/search passed. See `docs/guides/docs-beginner-review.md` for actual scope and limitations.
- The owned M6 fixture project, cache volume and unused dedicated runner image were removed; logs retained. The updated `pangolin-docs` service stays healthy at `http://localhost:18088/`.
- Product remains 0.1.0 preview. No remote push, tag, public deployment or version snapshot was performed. Local fixture recovery/load results do not promise production RPO, RTO or QPS.
