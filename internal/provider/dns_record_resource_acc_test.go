// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccDnsRecord exercises the full live lifecycle of one tfacc-prefixed
// TXT record in the test domain's active zone: create, in-place update,
// import round-trip, destroy. Run via `make testacc`.
func TestAccDnsRecord(t *testing.T) {
	testDomain := os.Getenv("COMLAUDE_TEST_DOMAIN")
	name := "tfacc-" + acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)

	config := func(ttl int, value string) string {
		return fmt.Sprintf(`
data "comlaude_domain" "test" {
  name = %q
}
resource "comlaude_dns_record" "test" {
  zone_id = data.comlaude_domain.test.active_zone_id
  name    = %q
  type    = "TXT"
  ttl     = %d
  value   = %q
}
`, testDomain, name, ttl, value)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(300, "tfacc initial"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("comlaude_dns_record.test", "id"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "fqdn", name+"."+testDomain),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "locked", "false"),
				),
			},
			{
				Config: config(600, "tfacc updated"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "ttl", "600"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "value", "tfacc updated"),
				),
			},
			{
				ResourceName:      "comlaude_dns_record.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["comlaude_dns_record.test"]
					return fmt.Sprintf("%s/%s/%s",
						rs.Primary.Attributes["group_id"],
						rs.Primary.Attributes["zone_id"],
						rs.Primary.ID), nil
				},
			},
		},
	})
}
