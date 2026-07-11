---
status: rework_fixes_verified
review: final_rereview_pending
ready_for_rereview: true
updated_at: 2026-07-12
---

# Integration Verification

## Scope

- Worktree: `F:\coding\new-api-wt-dashboard-quota-distribution`
- Sprint: `2026-07-12-dashboard-quota-distribution`
- Stage result: `rework fixes verified`, `final rereview pending`
- Commit policy: no git commit, no revert

## Rework Fix Audit

- P1 stable identity: `model/usedata_distribution.go` groups users by `user_id` and keys by `token_id`; renamed duplicate user rows collapse to one `user:1` row and current username is filled from `users`.
- P1 COUNT scan: distribution aggregation has no separate `.Count(` / `COUNT(` / `count(` scan in controller/model/router scope; negative-source detection is folded into the grouped aggregate SQL.
- P1 real component + route: `/dashboard/distribution` renders the real `DistributionSection`; a11y coverage checks controls, chart alternative text, table caption/headers, and live region.
- Locale fix: `zh` and `zh-TW` `Value` are both restored to `值`; i18n parity passes for all distribution/quota keys.
- Flow isolation: no flow paths are modified; added-line and new-file checks found zero flow imports/references.

## Validation Commands

- `go test ./controller ./model ./router` — PASS.
- `go test ./model -run "TestGetQuotaDistributionGroupsUsersByIDAndUsesCurrentUsername|TestGetQuotaDistributionGroupsKeysByTokenIDNotLabel" -count=1 -v` — PASS.
- `go test ./model -run "TestGetQuotaDistributionRejectsNegativeMetricsWithoutSeparateCountScan|TestQuotaDistributionAggregateSQLIsPortableAcrossDialects" -count=1 -v` — PASS.
- `bun test src/features/dashboard/api.test.ts src/features/dashboard/hooks/use-self-quota.test.ts src/features/dashboard/lib/distribution.test.ts src/features/dashboard/components/distribution/distribution-section.test.tsx src/features/dashboard/index.test.tsx src/features/dashboard/section-registry.test.ts src/features/wallet/lib/quota-refresh.test.ts src/features/dashboard/lib/flow.test.ts src/features/dashboard/lib/flow-selection.test.ts` — PASS, 53 tests.
- `bun test src/features/dashboard/components/distribution/distribution-section.test.tsx src/features/dashboard/index.test.tsx` — PASS, 8 tests after the locale fix.
- `bun run typecheck` — PASS.
- `bunx oxlint -c .oxlintrc.json <22 changed frontend ts/tsx files>` — PASS.
- `bunx oxfmt --check <29 changed frontend ts/tsx/json files>` — PASS after the locale fix.
- `bun run build` — PASS.
- `bun run build:check` — PASS after the locale fix.
- `bun run i18n:sync` — PASS; sync report regenerated.
- `node i18n parity check for en/zh/fr/ja/ru/vi/zh-TW over 30 distribution/quota keys` — PASS.
- `git diff --check` — PASS.

## Final Status

- Status: `READY_FOR_REREVIEW`.
- Remaining action: final rereview only; no commit performed.
