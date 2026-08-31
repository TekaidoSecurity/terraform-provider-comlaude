# Go client: generate from the OpenAPI spec, or hand-write?

> Research for issue #4. Decision itself belongs to the client-architecture ticket (#8).
> Spec under test: `https://api.comlaude.com/openapi/v1?docs.json` (OpenAPI 3.0.0, 288 paths / 372 operations, 417 schemas, 1,221 component parameters, 2.75 MB), fetched 2026-08-31. Codegen experiments run with oapi-codegen v2.8.0 (Go 1.27). All experiment artifacts live in the session scratchpad, not the repo.

## TL;DR

The spec does **not** run through oapi-codegen cleanly: it took six rounds of scripted spec surgery to reach generatable input, the full-spec output still does not compile, and only an output scoped to exactly the 16 v1 operations builds and vets cleanly (7.8k lines). Worse, the spec is internally inconsistent about the `{errors, messages, data}` envelope (90 response schemas type `data` as array, 70 as object), so generated types are wrong on the wire for one of those two groups no matter what. With ~15 endpoints in scope, **hand-write the client** and use the scoped generated output as a typing reference.

## Experiment: oapi-codegen on the raw spec

Command shape (config file with `generate: {models: true, client: true}`):

```
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest --config config.yaml comlaude-openapi.json
```

Every run below failed until the listed defect was patched by a preprocessing script; defects were discovered serially because each one masks the next.

| Round | Blocker | Scale | Fix applied |
|---|---|---|---|
| 1 | `"items": []` — empty array where a schema object is required; kin-openapi refuses to parse the document at all | 114 occurrences | rewrite to `{}` |
| 2 | `StandardResponse.data: []` — the **base envelope schema itself** uses an empty array as a schema | 1 | rewrite to `{}` |
| 3 | Parameters with no `schema` and no `content` (e.g. every `filter[...]` param) | **948 of 1,039** resolved parameters | inject `schema: {type: string}` |
| 4 | Duplicate query parameters on single operations (e.g. `filter[account.id]` declared twice) | 33 | dedupe |
| 5 | Path placeholders with no declared path parameter (`.../records/history/exports`) | 2 params on 1 path | inject path params |
| 6 | `enum: [null, 0, 1]` generates the literal Go token `<nil>` as a constant value | 6+ generated sites | strip `null`, mark `nullable` |
| 7 (compile) | Chained parameter `$ref`s (`DomainFilterRegisteredBefore` → `GeneralQueryFilterRegisteredBefore`): oapi-codegen references the alias type but never emits it → `undefined:` errors | 192 chained refs | flatten refs |

The preprocessing script ended up at roughly 100 lines of Python. It would have to live in the repo and be re-run — and possibly extended — on **every** spec refresh, because nothing stops Comlaude from shipping new invalidities.

### Output sizes and compile results (after all preprocessing)

| Scope | Config | Lines | Size | Compiles? |
|---|---|---|---|---|
| Full spec (372 ops) | no filter | 181,561 | 8.3 MB, one file | **No** — `PATCHProfileFormdataBody` enum typed string but constants `0`/`1`; `FilterContactId` field redeclared (`filter[contact.id]` vs `filter[contact_id]` sanitize to the same Go identifier, 6+ collisions) |
| 6 tags (`Auth`, `Users`, `Zones`, `Resource Records`, `Domains`, `Accounts`) via `output-options.include-tags` | tag filter | 48,470 | 2.1 MB | **No** — same `PATCH /profile` enum bug (the Users tag drags it in) |
| Exactly the 16 v1 operations via `output-options.include-operation-ids` | op filter | 7,828 | 348 KB | **Yes** — `go build` and `go vet` clean; 27 client methods |

So scoping works and works well: filtering by operation id transitively prunes unused schemas (417 → the ~40 actually referenced). The operation ids are ugly but stable and mechanical (`GET.groups.groupId.zones.zoneId.records` etc.). The tag route is not viable because the Users tag can't be included without the broken `PATCH /profile` operation, and there is no per-operation exclude that composes with include-tags cleanly.

### How the generated code handles the problem areas

**Form-urlencoded auth bodies: fine.** `POST /api_login` gets a `POSTApiLoginFormdataBody{ApiKey, Password, Username *string}` and a `...WithFormdataBody` method that goes through `runtime.MarshalForm` and sets the right content type. No complaints here.

**`filter[...]` query params: fine mechanically, fragile at scale.** The spec declares each filter as an individual `style: form` query parameter literally named `filter[name]` (not a deepObject). oapi-codegen generates one struct field per filter with the bracketed name in the `form:` tag, and serializes it correctly. But the field-name sanitizer collapses `filter[contact.id]` and `filter[contact_id]` to the same identifier, which is exactly what breaks the full-spec build. The v1 surface dodges every collision.

**The envelope: this is the deal-breaker.** The generated types faithfully mirror the spec — and the spec contradicts itself:

- List responses: `Data *[]ResourceRecord` — array, matching documented behavior.
- The login response: `Data *[]struct{AccessToken…}` — array (the token really arrives as `data[0]`).
- Single-object reads: `SuccessfulZoneViewResponse.Data *ZoneWithSupplier`, `SuccessfulViewDomainResponse.Data *Domain` — **object**, contradicting the documented always-an-array envelope (`docs/research/comlaude-api-summary.md`).

Across the whole spec, 90 response schemas type `data` as an array and 70 as an object. At least one group is wrong on the wire; if the always-array doc is right, every generated single-read type fails to unmarshal at runtime (`json: cannot unmarshal array into Go struct field`). Codegen cannot catch this — each endpoint's envelope shape needs verifying against the live API anyway, which erases most of the "trust the spec" benefit.

**Type quality inherited from the spec.** Every field is a pointer with `omitempty` (spec marks nothing required); `errors` and `messages` are `*[]interface{}` (94 `interface{}` occurrences in the 16-op output); several envelope structs use anonymous inline structs you can't name in function signatures; and spec typing bugs pass straight through — e.g. `ResourceRecord.RedirectPath *int` for a field the spec itself describes as "The source path of the URL from which to redirect". Terraform resource code would fight these types constantly.

## openapi-generator

Not runnable in this environment (Java/Docker unavailable), so no hands-on numbers. Structural facts from the project docs ([openapi-generator.tech/docs/generators/go](https://openapi-generator.tech/docs/generators/go)): it is a Java tool that emits a full Go module — one file per schema (417+ model files here), per-tag `api_*.go`, `configuration.go`, `client.go` — i.e. a substantially larger footprint than oapi-codegen for the same input, and its Go output style (mandatory getters/setters, `NullableX` wrappers) is heavier still. It validates specs on ingest, so the same invalidities from rounds 1–6 would need the same preprocessing; nothing about it sidesteps the envelope problem. No advantage over oapi-codegen for this API, with a JVM build dependency added.

## What comparable terraform-plugin-framework providers do

Verified against the repos themselves (go.mod, client packages, codegen markers):

| Provider | Client | Approach |
|---|---|---|
| [cloudflare/terraform-provider-cloudflare](https://github.com/cloudflare/terraform-provider-cloudflare) | `cloudflare-go` v6/v7 | **Fully generated** — SDK and provider both, by Stainless from Cloudflare's OpenAPI spec (`.stats.yml`: 2,470 endpoints). The [v5 upgrade guide](https://github.com/cloudflare/terraform-provider-cloudflare/blob/main/docs/guides/version-5-upgrade.md) calls it "a ground-up rewrite … using code generation from our OpenAPI spec". |
| [dnsimple/terraform-provider-dnsimple](https://github.com/dnsimple/terraform-provider-dnsimple) | [`dnsimple-go`](https://github.com/dnsimple/dnsimple-go) | **Hand-written** vendor SDK, conventional manual layout, no codegen markers. |
| [vercel/terraform-provider-vercel](https://github.com/vercel/terraform-provider-vercel) | in-repo `client/` package | **Hand-written, in-tree** — ~90 per-resource files with tests, no external SDK dependency at all. |
| [render-oss/terraform-provider-render](https://github.com/render-oss/terraform-provider-render) | in-repo `internal/client/` | **Generated with oapi-codegen v2** (`client_gen.go` header: "Code generated by …oapi-codegen/v2 v2.5.0"; depends on `oapi-codegen/runtime`). |

Patterns: generation correlates with API scale (Cloudflare's 2,470 endpoints) or with a vendor that owns and maintains its own clean spec (Render). Small-surface registrar/DNS providers (DNSimple) stay hand-written; the in-tree hand-written client (Vercel) is the standard pattern when no public Go SDK exists — which is Comlaude's situation. HashiCorp's own tutorials ([provider-configure](https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-provider-configure)) model a hand-written client; their [code-generation tech preview](https://developer.hashicorp.com/terraform/plugin/code-generation) generates provider schema/scaffolding from OpenAPI, not the API client.

## Maintenance weighing

- **v1 surface is ~16 operations** across 6 resource families. A hand-written client for this is on the order of 1–2k lines including tests — smaller than the *preprocessing-plus-config* apparatus codegen needs, and vastly smaller than the 7.8k-line generated file it would replace.
- **Spec drift**: regeneration only pays off if the spec is trustworthy. This spec is invalid in six distinct ways today and self-contradictory about its core envelope; every refresh means re-running (and possibly extending) the fixup script, then re-verifying envelope shapes against the live API. That is not cheaper than updating a hand-written client.
- **Hand-written drift**: with no sandbox environment (per the API summary) drift is caught the same way in both approaches — acceptance tests against the live API. Codegen adds no detection.
- **Where codegen still helps**: the compiling 16-op output is an excellent *reference* — field names, enums (the 12 record types), UUID formats — for writing the manual types. And if a later stage grows the surface to dozens of endpoints (orders, contacts, SSL), the scoped-generation route is proven to work and the fixup script already exists; the decision can be revisited then.

## Recommendation

**Hand-write a minimal in-tree client** (the Vercel pattern: `internal/client/` or `client/`, one file per resource family, hand-rolled envelope types with a custom `data` unmarshaler that accepts both array and object forms), covering only: `POST /api_login`, `POST /refresh`, `PUT /logout`, `GET /profile`, records CRUD, zones CRUD, domains read, accounts tree. Use the operation-scoped oapi-codegen output as the typing reference while writing it. Do not vendor generated code into the provider for v1.
