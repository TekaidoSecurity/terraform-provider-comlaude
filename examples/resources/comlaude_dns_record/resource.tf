data "comlaude_domain" "main" {
  name = "example.com"
}

resource "comlaude_dns_record" "www" {
  zone_id = data.comlaude_domain.main.active_zone_id
  name    = "www" # relative to the zone; "@" for the apex
  type    = "A"
  ttl     = 3600
  value   = "192.0.2.10"
}

resource "comlaude_dns_record" "mx" {
  zone_id  = data.comlaude_domain.main.active_zone_id
  name     = "@"
  type     = "MX"
  ttl      = 3600
  value    = "mail.example.com"
  priority = 10
}
