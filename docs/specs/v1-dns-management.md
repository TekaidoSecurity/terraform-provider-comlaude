# Spec: Comlaude Terraform Provider v1 — DNS Management

> The destination of the wayfinder map ["Comlaude Terraform provider v1 (DNS management)"](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/2).
> Every decision below was locked in a map ticket; the ticket holds the full rationale. Implementation should be sliceable from this document without reopening design questions.
> Assembled 2026-08-31.

## 1. What v1 ships

- Provider `comlaude`, address `registry.terraform.io/TekaidoSecurity/comlaude`, built on terraform-plugin-framework (repo already renamed and scaffolded — [ticket #9](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/9)).
- Resources: `comlaude_dns_record`, `comlaude_zone`.
- Data source: `comlaude_domain`.
- Hand-written API client in `internal/client/` ([ADR-0001](../adr/0001-hand-written-api-client.md)).
- CI: build + lint + unit/mocked tests + docs-drift, network-free. Acceptance tests run manually against the designated test domain.
- First release v0.1.0 → public repo → public Terraform Registry.

Out of scope for v1 (per the map): REDIRECT records (**Kering uses them — first post-v1 effort**), domain lifecycle via orders, nameserver sets, contacts, users, SSL, watches/brands/blocks, zone DNSSEC/secondary/networks management, BIND import/export.

## 2. API ground truth

Reference: [`docs/research/comlaude-api-summary.md`](../research/comlaude-api-summary.md); live-verified facts in [ticket #3](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/3).

- Base URL `https://api.comlaude.com`. Auth: `POST /api_login` (form-urlencoded `username`, `password`, `api_key`) → Bearer JWT, `expires_in` 7200s, plus refresh token (unused, see §4).
- Every response wraps payload as `{errors[], messages[], data, status_code}`. **`data` is an object on single reads and an array on collections — at runtime, regardless of what the spec claims per endpoint.** Never trust the spec's per-endpoint `data` typing.
- All v1 endpoints are group-scoped: `/groups/{groupId}/...`. Group scope cascades to child accounts.
- Requests are form-urlencoded. List endpoints paginate (`page`, `limit` ≤1000, default 25/page — lists must paginate to exhaustion).
- Live test fixtures: test domain `test-balenciaga.com` = `bff5c339-24f9-4496-a310-6b99d63dced2` (account "KERING - Testing" `ad3ccabe-…`, parent KERING `e635b379-…`; group `016592a1-793d-4218-bdd1-b2f25e89beae`); its one zone `ae4c5546-8d15-47d4-8b3a-f3b4fe3ef8a5` is **active**, holds 22 pre-existing records, `default_record_ttl` 86400. Service user has `zone-manager`, `domains-manager`, `manage resource records`.

## 3. Provider configuration

[Ticket #7](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/7) — exactly five attributes:

| Attribute | Required | Env fallback | Notes |
|---|---|---|---|
| `username` | yes (attr or env) | `COMLAUDE_USERNAME` | sensitive |
| `password` | yes (attr or env) | `COMLAUDE_PASSWORD` | sensitive |
| `api_key` | yes (attr or env) | `COMLAUDE_API_KEY` | sensitive |
| `base_url` | no | `COMLAUDE_BASE_URL` | default `https://api.comlaude.com`; the test seam |
| `group_id` | no | `COMLAUDE_GROUP_ID` | precedence: attribute > env > `GET /profile` `group_id`, resolved once at Configure and cached |

No other knobs in v1 (no logging/timeout attributes; timeouts are client-internal).

## 4. API client — `internal/client/`

[Ticket #8](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/8), [ADR-0001](../adr/0001-hand-written-api-client.md).

- **Hand-written**, one file per family: `auth.go`, `zones.go`, `records.go`, `domains.go` (+ `client.go`, `envelope.go`, `errors.go`). Constructor `client.New(baseURL, username, password, apiKey)`. Not a public SDK; no generated code vendored. The compiling 16-operation oapi-codegen output (see [`docs/research/go-client-generation.md`](../research/go-client-generation.md)) is a typing reference only.
- **Session**: eager `POST /api_login` at provider Configure (fail fast, clean diagnostic on bad credentials); token cached for process lifetime; `/refresh` unused; on a mid-run 401, re-login **once** and replay; single-flight mutex so Terraform's parallel operations share one token (one login per run).
- **Envelope**: generic `Envelope[T]` with a custom `data` unmarshaler accepting both object and array (unwrap `data[0]` when an array arrives for a single read). Resource code never sees `data`/`errors`/`messages`.
- **Typed errors** → diagnostics: `ErrNotFound` (404; on Read: remove from state), `ErrLocked` (423), `ErrPaymentRequired` (402: name the missing entitlement), `ErrRateLimited` (429), `ErrAuth` (401/403: name the required role, e.g. `zone-manager`), `ErrValidation` (422, carrying the API `errors[]`), `ErrServer` (5xx).
- **Retry** (hand-rolled in the client's `do()` loop): GET/PUT/DELETE on 429/500/502/503/504 and transport errors — exponential backoff + jitter, ~4 attempts, respect `Retry-After`. POST retries **only** on 429 or connection failure with no response received; **never on 5xx** (a retried create could duplicate records in a live zone). Per-request timeout ~30s.

## 5. Resource `comlaude_dns_record`

[Ticket #5](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/5). API: `POST/PUT/DELETE /groups/{gid}/zones/{zid}/records[/{rid}]`, `GET .../records` (list; **no single-record GET** — Read lists and filters by id, paginating).

One polymorphic resource:

| Attribute | Type | Behavior |
|---|---|---|
| `group_id` | string, optional | provider default; **ForceNew** |
| `zone_id` | string, required | **ForceNew** |
| `name` | string, required | **relative** (`"www"`); apex = `"@"`; client translates to/from API FQDN |
| `type` | string, required | enum: A, AAAA, CAA, CNAME, DS, NS, PTR, TXT, MX, MXDUMMY, SRV |
| `ttl` | int64, required | API min 1, max 604800 |
| `value` | string, required | format varies by type |
| `priority` | int64, optional | MX/MXDUMMY/SRV only |
| `weight`, `port` | int64, optional | SRV only |
| `flags` | int64, optional | CAA only; {0, 128} |
| `tag` | string, optional | CAA only; {issue, issuewild, iodef, contactemail} |
| `digest_type` | int64, optional | DS only; {1, 2, 4} |
| `key_tag` | int64, optional | DS only; 0–99999 |
| `algorithm` | int64, optional | DS only; {5,7,8,10,12,13,14,15,16} |
| `id`, `fqdn`, `locked` (bool) | computed | `locked` maps API 0/1 |

- Type-conditional validators reject type-foreign attributes at plan time.
- **All in-place updates** (PUT full-replace keeps the id) except `zone_id`/`group_id`.
- Import: `<group_id>/<zone_id>/<record_id>`.
- Docs must warn: changes in an active zone delegate to live DNS.
- **Verified** (tfacc-probe, 2026-08-31): create **requires FQDNs** — relative names are rejected 400 with a per-field `details` map ("The name must end with the current domain name"). Create returns only `{id}` (Read-after-create hydrates); single-record GET is 405 (Read paginates the list); the record's embedded `zone` object carries `domain.name`. The client translates relative↔FQDN via `ResolveZoneDomain` (record-embed → domain-list `active_zone` match → per-domain zone walk, cached per zone).

## 6. Resource `comlaude_zone`

[Ticket #6](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/6). API: `POST /groups/{gid}/domains/{did}/zones`, `GET/PATCH/DELETE .../zones/{zid}`. Create/delete need the `zone-manager` role; create can 402.

**Live-verified zone-create contract (2026-08-31, three clean rejections)**: `supplier_id` is REQUIRED on create (spec wrongly marks it optional); a domain holds at most ONE zone per supplier; and suppliers listed by `GET /suppliers` are not all entitled — the test account's two unused DNS suppliers were both rejected as "invalid", so no second zone can be created on the test domain at all. Consequence: the resource has an optional `supplier` attribute (name/key/id) with never-guess auto-resolution (sole candidate → automatic; several → error listing them); the live create path could not be acceptance-tested and is covered by mocks mirroring these rules — it should be verified once during the supervised pre-v0.1.0 check, ideally with Com Laude's guidance on a domain where creation is entitled.

| Attribute | Type | Behavior |
|---|---|---|
| `group_id` | string, optional | provider default; **ForceNew** |
| `domain_id` | string, required | **ForceNew** |
| `default_record_ttl` | int64, optional | in-place (PATCH); min 1, max 604800 |
| `active` | bool, optional, **default `false`** | in-place (PATCH) |
| `id`, `signed`, `networks` | computed | |

- **Destroy**: if the zone is active, fail with "zone is active; set `active = false` and apply before destroying". Never auto-deactivate. Inactive zones delete directly.
- Docs must warn: activating a zone **deactivates the domain's other zones** (they drift until next refresh); at most one active zone per domain.
- Import: `<group_id>/<domain_id>/<zone_id>`.

## 7. Data source `comlaude_domain`

[Ticket #7](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/7). API: `GET /groups/{gid}/domains?filter[name]=<name>` (exact match, live-verified).

- Config: `name` (required), `group_id` (optional override). Not-found ⇒ error.
- Computed exports: `id`, `account_id`, `account_name`, `management_status`, `registered_at`, `expires_at`, `tld`, `dnssec`, `nameservers` (list of hostnames), and from the embedded `active_zone`: **`active_zone_id`**, `active_zone_ttl`, `active_zone_record_count`.
- `active_zone_id` is the primary ergonomic path: `zone_id = data.comlaude_domain.main.active_zone_id`.
- No zone data source in v1.

## 8. Testing

[Ticket #12](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/12); standing split from the map Notes.

**CI (network-free)**: unit tests + HTTP-mocked tests against `httptest.Server` replaying fixtures from `internal/client/testdata/` — sanitized live captures (UUIDs randomized, domain replaced), refreshed via a `-capture` flag on acceptance runs. Mocks must cover both `data` envelope shapes, the 401-relogin-replay path, retry/backoff behavior, and the zone destroy-while-active diagnostic.

**Acceptance (manual, live)**:
- `make testacc`: parses `~/.config/comlaude/env` (key=value — **never `source`**; values are unquoted and may contain spaces) when `COMLAUDE_*` unset; sets `TF_ACC=1`; 30m timeout; **refuses to run without `COMLAUDE_TEST_DOMAIN`**.
- Record tests run in the test domain's **live active zone**; every created record named `tfacc-<random>`; tests touch only records they created by id; pre-existing records are never listed-and-modified.
- Zone tests create **inactive** zones and delete them on teardown. The harness **enforces `active != true` under `TF_ACC`** (guard in the test client): activating a test zone would deactivate the live zone and break its records. The `active` toggle + destroy-fails-when-active are mock-tested, plus **one supervised manual verification before v0.1.0** (human-driven, coordinated, immediately reverted).
- Sweepers keyed purely on the `tfacc-` prefix: sweep `tfacc-*` records anywhere in the test domain; sweep only inactive zones whose every record is `tfacc-*`. Documented as the post-failure recovery step.
- First smoke test carries the `tfacc-probe` name-semantics verification (§5).

## 9. CI workflow changes

[Ticket #10](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/10): edit `.github/workflows/test.yml` — **delete the `TF_ACC=1` acceptance matrix job**; keep build + golangci-lint, add `go test ./...`; keep the `make generate` docs-drift job. `release.yml` and `.goreleaser.yml` stay byte-for-byte as scaffolded.

## 10. Release & publishing

[Ticket #10](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/10):

1. Repo stays **private** during development; flips **public at v0.1.0**.
2. At release time, a wizard (pattern: `scripts/comlaude-credentials-wizard.sh`) generates a dedicated org GPG key (RSA 4096, "TekaidoSecurity Terraform Provider Signing"), stores `GPG_PRIVATE_KEY` + `PASSPHRASE` as Actions secrets, exports the public key.
3. Tag `v0.1.0` (only after the acceptance suite passes and the supervised activation check is done) → scaffolded goreleaser pipeline signs and publishes the GitHub Release.
4. One-time HITL registry onboarding: sign into registry.terraform.io as TekaidoSecurity, add the GPG public key to the namespace, publish the provider.
5. Manual `CHANGELOG.md` per release; goreleaser changelog stays disabled. v0.x until REDIRECT and the next wave land.

## 11. Cleanup obligations (MUST happen during implementation)

- Delete `internal/provider/example_*.go` (all example resources/data source/function/ephemeral/action and their tests) when the real implementations land.
- Delete `examples/*/comlaude_example/` and the generated `docs/*/example.md` pages; regenerate docs with tfplugindocs (`-provider-name comlaude` already set).
- Registry-facing docs: provider index (auth setup incl. API-key provisioning and the "web login" requirement), one example per resource/data source, import examples.
- **None of the scaffolding examples may survive to the first published release.**

## 12. Suggested implementation order

1. `internal/client/`: envelope + errors + retry + auth (mock-tested — the riskiest novelty first).
2. Provider Configure (5 attributes, group resolution, eager login).
3. `comlaude_dns_record` + mocked tests; first acceptance run incl. `tfacc-probe` (settles §5's open verification).
4. `comlaude_domain` data source; then `comlaude_zone` + its guard rails.
5. Harness completion: sweepers, `-capture`, `make testacc`; CI surgery (§9).
6. Cleanup (§11), docs generation, CHANGELOG.
7. Release runbook (§10).

## 13. Glossary & decisions index

Vocabulary: [`CONTEXT.md`](../../CONTEXT.md) (Group, Account, Domain, Zone, Apex, Resource Record, Domain Order, Envelope). Architecture: [`docs/adr/0001-hand-written-api-client.md`](../adr/0001-hand-written-api-client.md). Full decision trail: the map's [Decisions so far](https://github.com/TekaidoSecurity/terraform-provider-comlaude/issues/2).
