## 0.1.0

Initial release: DNS management for the Com Laude corporate registrar.

FEATURES:

* **New resource** `comlaude_dns_record`: manages a single DNS record (A, AAAA, CAA, CNAME, DS, NS, PTR, TXT, MX, MXDUMMY, SRV) with relative names (`"@"` for the apex), type-conditional validation, in-place updates, and import.
* **New resource** `comlaude_zone`: manages a DNS zone — created inactive by default, explicit activation, deliberate two-step teardown for active zones, automatic DNS-supplier resolution when unambiguous, and import.
* **New data source** `comlaude_domain`: looks up a domain by exact name and exports its attributes, including `active_zone_id` for record-only configurations.
* Provider authentication via Com Laude `POST /api_login` (username, password, API key — environment-variable fallbacks), with automatic default-group resolution.

NOTES:

* REDIRECT records are not yet supported (planned for a future release).
* Zone activation (`active = true`) is covered by mocked tests; it deactivates the domain's other zones and should be exercised deliberately.
