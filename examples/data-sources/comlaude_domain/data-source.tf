data "comlaude_domain" "main" {
  name = "example.com"
}

# The usual way to address the zone serving live DNS:
output "active_zone_id" {
  value = data.comlaude_domain.main.active_zone_id
}
