# Wallet Top-Up Affiliate Rebate Plan

## Goal

Add a wallet-only affiliate rebate: when an invited user completes a wallet top-up, the inviter receives a configurable percentage of the actual credited quota as affiliate balance. Also expose structured invite and rebate logs in the wallet referral plan UI.

## Non-Goals

- Do not apply rebate to subscription purchases.
- Do not apply rebate to redemption codes, check-ins, task refunds, normal refunds, or admin quota edits.
- Do not rewrite payment provider integrations beyond routing wallet crediting through a model-layer helper.

## Existing Context

- User referral fields exist in `model.User`: `AffCode`, `AffCount`, `AffQuota`, `AffHistoryQuota`, `InviterId`.
- Existing fixed invite rewards use `QuotaForInviter` and `QuotaForInvitee`.
- Affiliate balance can already be transferred through `/api/user/aff_transfer`.
- Wallet top-up success paths are split across model and controller code.

## Configuration

Add `RechargeRebateRatioForInviter`:

- Type: `float64` runtime setting, persisted through `OptionMap`.
- Semantics: percent value. `5` means 5%; `0` disables.
- Valid range: `0 <= value <= 100`.
- Backend validation belongs in option update handling.
- Frontend validation belongs in system settings quota schema.

Touched backend files:

- `common/constants.go`
- `model/option.go`
- `controller/option.go`

Touched frontend files:

- `web/default/src/features/system-settings/types.ts`
- `web/default/src/features/system-settings/billing/index.tsx`
- `web/default/src/features/system-settings/billing/section-registry.tsx`
- `web/default/src/features/system-settings/general/quota-settings-section.tsx`
- `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`

## Wallet Credit Abstraction

Introduce a model-layer wallet credit helper, for example:

```go
type WalletTopUpCreditResult struct {
    UserId        int
    TradeNo       string
    PaymentMethod string
    Provider      string
    Money         float64
    QuotaToAdd    int
    RebateQuota   int
    InviterId     int
}

func creditWalletTopUpTx(tx *gorm.DB, topUp *TopUp, quotaToAdd int, expectedProvider string) (*WalletTopUpCreditResult, error)
```

Responsibilities:

1. Run only inside the existing top-up completion transaction.
2. Assert `topUp.Status == TopUpStatusPending`.
3. Assert `expectedProvider` is in the wallet top-up provider allowlist.
4. Assert `topUp.PaymentProvider == expectedProvider`; do not weaken existing provider-specific mismatch protection.
5. Add `quotaToAdd` to the invitee wallet quota.
6. Apply inviter rebate if enabled.
7. Write structured affiliate rebate log.
8. Return log metadata for normal top-up logs and cache updates after commit.

Why this shape:

- It isolates wallet recharge accounting from payment provider details.
- It avoids unsafe global hooks on `IncreaseUserQuota`, which is also used by check-ins, refunds, task refunds, and admin operations.
- It keeps idempotency bound to the existing `TopUpStatusPending -> TopUpStatusSuccess` transition.
- It prevents accidental use from subscription completion by checking provider/status and by keeping subscription order handling out of scope.

## Rebate Calculation

```text
rebateQuota = floor(quotaToAdd * RechargeRebateRatioForInviter / 100)
```

Rules:

- If ratio is `0`, no rebate.
- If invitee has no `InviterId`, no rebate.
- If inviter equals invitee, no rebate.
- If computed rebate is `<= 0`, no rebate.
- Use decimal arithmetic for the ratio calculation.
- Credit inviter `AffQuota` and `AffHistoryQuota`.
- Write a `topup_rebate` affiliate log with `IdempotencyKey = "topup_rebate:" + tradeNo`.
- If the idempotency row already exists, treat it as an idempotent no-op only when the top-up is already success; otherwise return an error so the transaction does not silently diverge.

## Cache Handling

Quota cache must remain correct after wallet crediting.

- Existing `IncreaseUserQuota` updates cache, but several wallet top-up paths currently update DB directly.
- The new helper may update DB inside the transaction, but it must return the credited invitee ID and delta so the caller can update or invalidate quota cache after commit.
- Preferred implementation: add a model-level post-commit cache helper and call it after the transaction succeeds.
- Do not update Redis cache before transaction commit.
- Current user quota cache must be invalidated or refreshed for the invitee after commit; prefer invalidation or full refresh over pre-commit mutation.
- Inviter affiliate balance is not part of the current quota cache. If a future profile cache includes `AffQuota`, invalidate/update it after commit as well.

## Structured Affiliate Logs

Add `AffiliateLog`, for example in `model/affiliate_log.go`:

```go
type AffiliateLog struct {
    Id            int
    InviterId     int
    InviteeId     int
    Type          string
    TradeNo       string
    IdempotencyKey string
    RewardQuota   int
    BaseQuota     int
    RebatePercent float64
    CreatedAt     int64
}
```

The concrete GORM field for `IdempotencyKey` should be bounded and unique, for example:

```go
IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(191);not null;uniqueIndex"`
```

`varchar(191)` avoids MySQL index-length surprises while remaining portable to SQLite and PostgreSQL.

Types:

- `invite_reward`: fixed reward granted when an invited user registers.
- `topup_rebate`: percentage rebate granted when an invited user completes a wallet top-up.

Indexes:

- `inviter_id`, `created_at` for referral-plan list queries.
- Unique guard by `idempotency_key`, not by `type + trade_no`.
- `invite_reward` uses an idempotency key such as `invite_reward:{inviter_id}:{invitee_id}`.
- `topup_rebate` uses an idempotency key such as `topup_rebate:{trade_no}`.
- This avoids ordinary invite logs colliding on an empty trade number.
- The idempotency key must be non-empty for every row and should use a normal unique index compatible with SQLite, MySQL, and PostgreSQL.

Touched files:

- `model/affiliate_log.go`
- `model/main.go`
- `model/user.go`
- `model/topup.go`

## Wallet Top-Up Paths

Apply the wallet credit helper to:

- Stripe wallet top-up: `model.Recharge`.
- Creem wallet top-up: `model.RechargeCreem`.
- Waffo wallet top-up: `model.RechargeWaffo`.
- Waffo Pancake wallet top-up: `model.RechargeWaffoPancake`.
- Admin manual completion: `model.ManualCompleteTopUp`.
- Epay wallet top-up: keep controller verification logic, but replace the success branch accounting with a model-layer wallet completion helper.

Manual completion semantics:

- Manual completion is an admin completion path for an existing wallet top-up order, not a distinct `PaymentProvider`.
- It must preserve and use the order's original `PaymentProvider` as `expectedProvider`.
- The original provider must still be in the wallet top-up provider allowlist.
- Quota base calculation follows the original provider's wallet top-up rules; for example, manually completing a Stripe wallet order uses the Stripe `money * QuotaPerUnit` base.

Do not touch:

- `model.CompleteSubscriptionOrder`.
- Redemption code flows.
- Check-in quota award.
- Refund and task refund flows.

## Epay Minimal Change

Do not rewrite Epay verification or callback parsing.

Add a model function such as:

```go
func CompleteEpayWalletTopUp(tradeNo string, actualPaymentMethod string, callerIp string) error
```

The controller keeps:

- signature verification;
- trade status handling;
- order lock;
- payment-provider guard.
- response writing.

The model function owns:

- row lock;
- pending-status check;
- actual payment method update;
- `pending -> success`;
- quota credit;
- inviter rebate;
- structured rebate log.

Important response-order rule:

- Do not send Epay `success` before local accounting succeeds.
- If `CompleteEpayWalletTopUp` fails, return failure or do not acknowledge success so the provider can retry according to existing Epay behavior.
- This is the only required Epay behavior change; verification and parsing remain unchanged.

## Referral Plan API

Add current-user endpoints under authenticated user routes, for example:

- `GET /api/user/affiliate/logs?type=invite_reward&page=1&page_size=20`
- `GET /api/user/affiliate/logs?type=topup_rebate&page=1&page_size=20`

Rules:

- Only returns logs where `inviter_id` is current user ID.
- Supports pagination.
- Optional `type` filter only accepts known affiliate log types.

Touched files:

- `controller/user.go`
- `router/api-router.go`
- `model/affiliate_log.go`

## Frontend UI

System settings, quota section:

- Add `RechargeRebateRatioForInviter`.
- Label: `Wallet top-up rebate for inviter (%)`.
- Description: `Percentage of an invited user's successful wallet top-up credited to the inviter's affiliate balance. 0 disables it.`
- Validate with `z.coerce.number().min(0).max(100)`.

Wallet referral plan:

- Add logs area with two tabs:
  - Invite logs.
  - Rebate logs.
- Invite log columns: invitee, reward quota, created time.
- Rebate log columns: invitee, trade number, base quota, rebate percent, reward quota, created time.

## Parallel Work Plan

### Worker A: Backend Config

Owns:

- `common/constants.go`
- `model/option.go`
- `controller/option.go`

Tasks:

- Add runtime setting.
- Wire OptionMap load/save.
- Add server validation for `0..100`.
- Add focused option validation tests.

### Worker B: Affiliate Log Model

Owns:

- `model/affiliate_log.go`
- `model/main.go`
- invite-log writes in `model/user.go`

Tasks:

- Add model and migration.
- Add query helper and response DTO shape for current inviter logs.
- Write `invite_reward` log when fixed invite reward is granted.
- Keep `invite_reward` log creation atomic with the existing inviter reward update where practical, covering both normal registration and OAuth finalization paths.
- Implement idempotency key generation and unique index.

### Worker C: Wallet Credit + Rebate Core

Owns:

- `model/topup.go`
- `controller/topup.go` only for the minimal Epay success-branch accounting and response-order change

Tasks:

- Add wallet credit helper.
- Add rebate calculation and grant.
- Add `topup_rebate` log write.
- Wire Stripe, Creem, Waffo, Waffo Pancake, and manual completion.
- Add Epay model accounting function for controller to call.
- Replace Epay controller success-branch accounting with the model function and ensure success is acknowledged only after local accounting commits.

### Worker D: Referral Plan API

Owns:

- `controller/user.go`
- `router/api-router.go`

Tasks:

- Add affiliate log endpoint.
- Add pagination and type validation.
- Ensure only current inviter's logs are returned.
- Consume Worker B's query helper and response shape; do not edit `model/affiliate_log.go` unless integration requires a small adapter.

### Worker E: Frontend

Owns:

- system-settings quota files;
- wallet referral-plan files;
- i18n locale files.

Tasks:

- Add settings field and validation.
- Add affiliate logs API/types/hooks.
- Add invite/rebate log tabs to wallet referral plan.
- Add translations for all supported frontend locales.

## Integration Order

1. Worker A and Worker B can run in parallel.
2. Worker C starts after A confirms setting name and B confirms log model.
3. Worker D starts after B query shape is stable.
4. Worker E can add system setting immediately, then finish wallet logs after D response shape is stable.
5. Integrate Epay as the final backend accounting path because it currently has controller-level accounting.

## Tests

Backend:

- Ratio `0` disables rebate.
- Ratio `5` grants 5% of actual credited quota.
- Ratio `100` grants full actual credited quota.
- No inviter means no rebate.
- Replayed webhook or already-success order does not grant duplicate rebate.
- Subscription order does not grant rebate.
- Epay success path credits wallet and rebate through model accounting.
- Epay local accounting failure does not respond with `success`.
- Invalid setting values below 0 or above 100 are rejected.
- Invite logs do not collide when multiple invitees register without trade numbers.
- `topup_rebate` idempotency key prevents duplicate rebate for the same trade number.
- Check-in, redemption code, refund/task refund, and admin direct quota edits do not create `topup_rebate`.
- Manual top-up completion grants at most one rebate even if called twice.
- Helper-level table tests cover provider-specific quota bases for Stripe, Creem, Waffo, Waffo Pancake, Epay, and manual completion using the order's original provider.
- Provider mismatch tests ensure each wallet completion path rejects orders whose `PaymentProvider` does not equal the expected provider.
- Quota cache is updated or invalidated only after successful transaction commit.

Frontend:

- Settings form rejects values above 100 and below 0.
- Wallet referral plan renders invite and rebate logs from API response.
- The current frontend has no Vitest/Jest/Testing Library setup or `test` package script. Use Bun's built-in test runner for minimal executable schema and server-rendered component tests, and keep build/lint/i18n verification as the broader frontend safety net.

Verification:

```bash
go test ./model ./controller
cd web/default && bun run build
```

## Risks

- Epay currently performs accounting in the controller; moving only the accounting branch is the safest minimal change.
- Existing direct `Update("quota", ...)` calls must be replaced only for wallet top-ups, not unrelated quota changes.
- Existing successful historical orders should not be backfilled unless explicitly requested.
- Unique constraints must stay compatible with SQLite, MySQL, and PostgreSQL. Use a non-empty `idempotency_key` unique index instead of nullable/partial indexes.
- Epay must not acknowledge success before local accounting commits.
