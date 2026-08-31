# Comlaude Terraform Provider

A Terraform provider for the Com Laude corporate registrar API, starting with DNS management (zones and records) and growing toward the wider registrar surface.

## Language

**Group**:
The tenant scope every API call lives under (`/groups/{groupId}`); a node in the Com Laude account hierarchy. The provider takes a default `group_id` with per-resource override.
_Avoid_: tenant, organisation

**Account**:
A Group that can hold domains; accounts form a tree under a root Group.

**Domain**:
A registered domain name held in an Account. Read-only in v1 (data source); its lifecycle is driven by Domain Orders, not direct CRUD.

**Zone**:
A DNS zone attached to a Domain, holding Resource Records. A zone can be **active** (its record changes are delegated to live DNS) and cannot be deleted while active.
_Avoid_: DNS zone file (the BIND export is a representation, not the zone)

**Resource Record**:
A single DNS entry inside a Zone (A, AAAA, CNAME, MX, TXT, SRV, CAA, DS, NS, PTR, plus Com Laude's MXDUMMY and REDIRECT pseudo-types).
_Avoid_: DNS entry, record set (records are individual, not grouped by name/type)

**Domain Order**:
The async job object through which all domain lifecycle mutations happen (registration, transfer, renewal, lapse, locks). Polled for completion; never updated or cancelled. Out of scope for v1.
_Avoid_: job, task

**Envelope**:
The `{errors, messages, data[], status_code}` wrapper on every API response; `data` is an array even for single objects.
