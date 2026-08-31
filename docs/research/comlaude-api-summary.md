# Com Laude API — OpenAPI summary for Terraform provider design

> Source: `https://api.comlaude.com/openapi/v1?docs.json` (OpenAPI 3.0.0, 288 paths / 372 operations, fetched 2026-08-31).
> This is the research base for the wayfinder map "Comlaude Terraform provider v1".

## Authentication

Single security scheme: **HTTP Bearer token** (`Authorization: Bearer <access_token>`), applied globally.

Token acquisition (all bodies `application/x-www-form-urlencoded`):

| Endpoint | Purpose |
|---|---|
| `POST /api_login` | **The automation path.** Body: `username`, `password`, `api_key`. Bypasses MFA, returns a fully validated access token + refresh token. Only works for users with the "web login" property enabled. |
| `POST /login` | Interactive: returns a partially validated token; must complete with `POST /two_factor_codes`. |
| `POST /refresh` | Body: `refresh_token`. New access + refresh tokens. |
| `PUT /logout` | Invalidates session. |
| `GET /.well-known/jwks.json` | JWKS — access tokens are JWTs. |

Token response: `data[0] = { token_type: "Bearer", expires_in: 7600, access_token, refresh_token }` (~2h expiry).

API keys: `POST /users/{userId}/api_key` (a user has exactly ONE key; re-posting replaces it), `GET /users/{userId}/api_key`. The api_key is a third input to `/api_login`, not a bearer credential itself.

**Multi-tenancy:** almost everything is scoped under `/groups/{groupId}/...`. No `GET /groups` list — hierarchy discovered via `GET /groups/{groupId}/accounts/tree` or `GET /profile`.

**Response envelope:** everything wrapped in `{ errors: [], messages: [], data: [...], status_code }`; `data` is an array even for single objects.

## Resource families relevant to v1 (DNS management)

### DNS Resource Records — full CRUD (cleanest resource in the API)

- `GET /groups/{groupId}/zones/{zoneId}/records` — list
- `POST /groups/{groupId}/zones/{zoneId}/records` — create. Required: `name`, `type`, `ttl`, `value`; type enum: **A, AAAA, CAA, CNAME, DS, NS, PTR, TXT, MX, MXDUMMY, REDIRECT, SRV**; plus `priority`, `weight`, `port`, `flags`, `tag`, `digest_type`, `key_tag`, `algorithm`, and REDIRECT options (`redirect_mode`, `redirect_path`, `redirect_https_*`). "If the zone is active this will result in specified changes being delegated to the DNS."
- `PUT .../records/{recordId}` — update (full replace)
- `DELETE .../records/{recordId}` — delete
- Cross-zone reads/search/history/exports also exist (exports are 202 email jobs).

Note: records are addressed by `groupId` + `zoneId` + `recordId` (no domainId in the path).

### DNS Zones — CRUD with caveats

- `GET /groups/{groupId}/domains/{domainId}/zones` — list zones of a domain
- `POST /groups/{groupId}/domains/{domainId}/zones` — create (requires **zone-manager role**; optional `default_record_ttl`; **402 Payment Required** possible)
- `GET .../zones/{zoneId}` — read
- `PATCH .../zones/{zoneId}` — update (**only** `default_record_ttl`, applies to new records)
- `DELETE .../zones/{zoneId}` — **only allowed on zones not marked active** (zone-manager role)
- `GET .../zones/{zoneId}/bind` — BIND export; `POST .../zones/parse_bind`, `POST .../zones/import`
- History endpoints at zone/domain/group level (read-only audit)

### Domains — CRU, no DELETE (v1: read-only data source)

- `GET /groups/{groupId}/domains` — list (rich `filter[...]` params, pagination)
- `GET /groups/{groupId}/domains/{domainId}` — read
- `POST /groups/{groupId}/domains/search` — filtered search in body
- Create via `POST /domains` or a `registration` domain order; update via PATCH; **no DELETE** — lapse/registration/transfer/renewal/locks go through the async **domain_orders** engine (`POST /groups/{groupId}/domain_orders`, poll `GET .../{orderId}` for `status`/`completed_at`; item-level `retry`). Out of scope for v1.

### Accounts / Groups

- `GET /groups/{groupId}/accounts` — child accounts; `GET /groups/{groupId}/accounts/tree` — full hierarchy; `GET /profile` — current user (route into group ids).
- Full CRUD on accounts exists (delete is soft, dependency-blocked) — out of scope for v1 beyond group resolution.

## Out-of-scope families (later stages)

Nameserver sets (full CRUD), contacts (full CRUD; delete can 423, update can 202 async, NIS2 approval workflows), users + RBAC, domain/contact orders, SSL certificate orders (revoke-only destroy), watches/brands (no delete), blocks (read/update only), invoicing, suppliers/products, TLDs (read-only), reference data (countries/currencies/services), stats.

## CRUD gaps summary (v1-relevant)

| Concept | Gap | Consequence |
|---|---|---|
| Zone | DELETE only when not active; PATCH only `default_record_ttl` | Destroy may fail on active zones; most attrs ForceNew |
| Domain | No DELETE; two mutation channels (PATCH vs orders) | v1 keeps it read-only (data source) |
| Glue records | Read-only; mutations via order actions | Out of scope v1 |
| DNSSEC keys | POST only | Out of scope v1 |

## Async / eventual-consistency signals

- ~20 `POST .../exports` endpoints return **202**; results are emailed, not retrievable via API.
- `domain_orders` / `contact_orders` are the canonical async job objects (poll `status`).
- **No webhooks anywhere in the spec.** Polling is the only completion signal.
- Registrar passthrough endpoints (`registrar.domain.*`, `domains/check`) are synchronous but document 500/504 — live registrar backends; needs timeouts/retries.
- Only `POST .../certificate_orders/preview` documents a 429, but defensive 429 handling is warranted everywhere.

## Identifiers

- **UUID strings** for Domain, Account/Group, Contact, Zone, ResourceRecord, NameserverSet, User, Order ids.
- Composite addressing: nearly everything needs `groupId` + own id; records: `groupId`+`zoneId`+`recordId`; zones: `groupId`+`domainId`+`zoneId`. Terraform import IDs will be multi-part.

## Other notes

- Pagination: `page`, `limit` (1–1000), `sort`, `fields`; `Pagination` meta with totals and links. Every list has a `POST .../search` twin taking filters in the body.
- Error statuses seen across the spec: 400, 401, 402, 403, 404, 422, 423, 429, 500, 504.
- Roles gate endpoints: zone create/delete needs the `zone-manager` role.
- **No sandbox/test environment mentioned**; single unnamed server (base `https://api.comlaude.com`); no API versioning in paths.
- `POST /groups/{groupId}/domain_orders/preview` exists (dry-run validation + pricing) — a natural plan-time helper for later stages.
