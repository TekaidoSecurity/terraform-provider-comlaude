# Hand-written in-tree API client, no OpenAPI codegen

Com Laude publishes a full OpenAPI spec, so a generated client looks like the obvious path — we deliberately rejected it. The spec is invalid in six distinct ways (oapi-codegen needs ~100 lines of preprocessing before it parses), the full-spec output doesn't compile, and the spec contradicts itself about the `{errors, messages, data}` envelope in 160 response schemas — a contradiction confirmed against the live API, where `data` arrives as an object on single reads and an array on collections. With only ~16 operations in v1 scope, we hand-write `internal/client/` (the Vercel pattern), with a generic envelope type whose unmarshaler accepts both `data` shapes. Full evidence: `docs/research/go-client-generation.md`.

## Consequences

- The operation-scoped oapi-codegen output (which does compile) serves as a typing reference only; no generated code is vendored.
- Revisit if a later stage grows the surface substantially (orders, contacts, SSL): scoped generation is proven viable and the spec-fixup script exists.
- Client seam for tests is the base URL (`base_url` provider attribute / `COMLAUDE_BASE_URL`), pointed at `httptest` servers replaying live-captured envelopes.
