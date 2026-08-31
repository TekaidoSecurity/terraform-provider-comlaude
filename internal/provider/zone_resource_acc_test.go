// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccZone exercises the live lifecycle of one INACTIVE zone on the test
// domain: create, in-place TTL update, import round-trip, destroy. The zone
// never serves DNS — `active` stays at its false default throughout, and the
// client's TF_ACC guard makes activation impossible even by accident.
// Run via `make testacc`.
func TestAccZone(t *testing.T) {
	testDomain := os.Getenv("COMLAUDE_TEST_DOMAIN")
	// The test account offers several DNS suppliers, so the resource's
	// ambiguity rule (never guess) requires an explicit choice here.
	supplier := os.Getenv("COMLAUDE_TEST_SUPPLIER")
	if supplier == "" {
		t.Skip("COMLAUDE_TEST_SUPPLIER not set: the test account has multiple DNS suppliers, " +
			"and zone creation deliberately refuses to guess (e.g. set it to \"Com Laude DNS\")")
	}

	config := func(ttl int) string {
		return fmt.Sprintf(`
data "comlaude_domain" "test" {
  name = %q
}
resource "comlaude_zone" "test" {
  domain_id          = data.comlaude_domain.test.id
  supplier           = %q
  default_record_ttl = %d
}
`, testDomain, supplier, ttl)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(3600),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("comlaude_zone.test", "id"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "active", "false"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "default_record_ttl", "3600"),
				),
			},
			{
				Config: config(7200),
				Check:  resource.TestCheckResourceAttr("comlaude_zone.test", "default_record_ttl", "7200"),
			},
			{
				ResourceName:      "comlaude_zone.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["comlaude_zone.test"]
					return fmt.Sprintf("%s/%s/%s",
						rs.Primary.Attributes["group_id"],
						rs.Primary.Attributes["domain_id"],
						rs.Primary.ID), nil
				},
			},
		},
	})
}
