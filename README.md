# Terraform Provider for Com Laude

Manage DNS on the [Com Laude](https://comlaude.com) corporate registrar as code: DNS records and zones, with domain lookup.

- **Resources**: `comlaude_dns_record`, `comlaude_zone`
- **Data source**: `comlaude_domain`
- Registry documentation lives in [`docs/`](docs/index.md); runnable configuration in [`examples/`](examples/).

```hcl
provider "comlaude" {} # credentials via COMLAUDE_USERNAME / COMLAUDE_PASSWORD / COMLAUDE_API_KEY

data "comlaude_domain" "main" {
  name = "example.com"
}

resource "comlaude_dns_record" "www" {
  zone_id = data.comlaude_domain.main.active_zone_id
  name    = "www"
  type    = "A"
  ttl     = 3600
  value   = "192.0.2.10"
}
```

Authentication uses Com Laude's `POST /api_login` and needs three credentials (username, password, API key) for a service user with the "web login" property enabled.

## Development

- Go >= 1.24 and Terraform >= 1.0.
- `go build ./...` builds; `make test` runs the unit and HTTP-mocked suites (no credentials, no live API traffic).
- `make generate` regenerates registry docs (tfplugindocs); CI fails on drift.
- `make testacc` runs acceptance tests **against the live API**, strictly scoped to the designated test domain; it refuses to run without `COMLAUDE_TEST_DOMAIN`. See `scripts/testacc.sh`. Credentials are collected once by `scripts/comlaude-credentials-wizard.sh` into `~/.config/comlaude/env`.
- Design decisions: [`docs/adr/`](docs/adr/) and the v1 spec at [`docs/specs/v1-dns-management.md`](docs/specs/v1-dns-management.md).
