data "comlaude_domain" "main" {
  name = "example.com"
}

# Zones are created inactive; serving live DNS is an explicit opt-in.
# When several DNS suppliers are available for the domain, set `supplier`
# (by name, key, or id) - otherwise the sole candidate is chosen
# automatically.
resource "comlaude_zone" "staging" {
  domain_id          = data.comlaude_domain.main.id
  default_record_ttl = 3600

  # active = true # deactivates the domain's other zones - deliberate act
}
