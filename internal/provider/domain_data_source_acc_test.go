// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDomainDataSource resolves the designated test domain against the
// live API (read-only). Run via `make testacc`.
func TestAccDomainDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "comlaude_domain" "test" {
  name = %q
}
`, os.Getenv("COMLAUDE_TEST_DOMAIN")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.comlaude_domain.test", "id"),
					resource.TestCheckResourceAttrSet("data.comlaude_domain.test", "account_id"),
					resource.TestCheckResourceAttrSet("data.comlaude_domain.test", "active_zone_id"),
					resource.TestCheckResourceAttrSet("data.comlaude_domain.test", "expires_at"),
				),
			},
		},
	})
}
