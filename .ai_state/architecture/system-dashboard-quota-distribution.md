# Dashboard Quota Distribution

## Purpose

Provide an independent dashboard surface for quota, token, and request concentration while preserving the existing Flow analytics contract and implementation.

## Backend Endpoints

- `GET /api/user/self/quota` is registered under the authenticated user route. `controller.GetSelfQuota` validates the context user id and returns `model.GetUserQuota(userID, false)` as `{ quota }`.
- `GET /api/data/distribution` is registered under `middleware.UserAuth()`. The controller accepts `start_timestamp`, `end_timestamp`, `metric`, `dimension`, and the admin-only username filter, then passes authenticated id and role to the model.

## Distribution Authorization

- Common user: `key` and `group`; every query is constrained by the authenticated `user_id` regardless of forged query parameters.
- Admin: `user` and `group`; `key` is rejected.
- Root: `user`, `key`, and `group` globally.
- Authorization is enforced in `model/usedata_distribution.go`, not inferred from frontend selector visibility.

## Data Flow

1. The controller validates time range, metric, dimension, authenticated id, and role context.
2. The model applies role scope and groups log rows by stable identity: `user_id`, `token_id`, or group.
3. One grouped SQL query returns quota, token, request, and negative-source detection data; no separate count scan is used.
4. User and key labels are filled from current records without changing stable grouping identity.
5. The frontend normalizes rows into one canonical Top 8 plus Other segment list used by both chart and table.

## Frontend State and Rendering

- Self quota uses React Query key `['user', 'self', 'quota']`; the query cache is the overview balance card's remote source of truth.
- Distribution requests use `['dashboard', 'distribution', ...filters]` and retain previous data during filter refreshes.
- Confirmed quota-changing wallet actions invalidate self quota; order creation alone does not.
- `DistributionSection` renders a VChart donut and an accessible ranking table from the same aggregation result. Zero totals hide the donut and render the empty state.

## Flow Isolation

- Distribution has its own endpoint, DTOs, pure aggregation library, React Query key, component, route section, and tests.
- Existing `/api/data/flow` and `/api/data/flow/self` routes remain unchanged.
- Distribution code does not import or reuse Flow DTOs, aggregation functions, components, tests, or state. Shared usage is limited to generic dashboard infrastructure, common storage semantics, formatting, and base UI/chart primitives.

## Verification Boundary

- Backend tests cover permission scope, stable user/key identities, negative metrics, and SQL portability across SQLite, MySQL, and PostgreSQL expectations.
- Frontend tests cover endpoint parameters, query policy, aggregation, permissions, route rendering, accessibility, wallet invalidation, and Flow isolation.
