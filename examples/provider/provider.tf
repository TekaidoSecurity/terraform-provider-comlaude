terraform {
  required_providers {
    comlaude = {
      source = "TekaidoSecurity/comlaude"
    }
  }
}

# Credentials are usually supplied via the environment:
#   COMLAUDE_USERNAME, COMLAUDE_PASSWORD, COMLAUDE_API_KEY
# and optionally COMLAUDE_GROUP_ID. The service user must have the
# "web login" property enabled and an API key created.
provider "comlaude" {}
