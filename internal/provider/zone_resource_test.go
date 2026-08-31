// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"regexp"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func zoneConfig(baseURL, extra string) string {
	return fmt.Sprintf(`
provider "comlaude" {
  base_url = %q
  group_id = "g-1"
}
resource "comlaude_zone" "test" {
  domain_id = "d-1"
  %s
}
`, baseURL, extra)
}

func TestZoneTTLAndActiveUpdateInPlace(t *testing.T) {
	fake, srv := newFakeZoneAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: zoneConfig(srv.URL, `default_record_ttl = 3600`)},
			{
				Config: zoneConfig(srv.URL, "default_record_ttl = 7200\n  active = true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("comlaude_zone.test", "default_record_ttl", "7200"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "active", "true"),
				),
			},
			// Deactivate again so the framework's final destroy succeeds.
			{Config: zoneConfig(srv.URL, `default_record_ttl = 7200`)},
		},
	})

	if got := fake.createCount(); got != 1 {
		t.Errorf("creates = %d, want 1 (ttl/active changes must be in-place PATCHes)", got)
	}
}

func TestZoneDestroyWhileActiveFailsWithTwoStepDiagnostic(t *testing.T) {
	_, srv := newFakeZoneAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: zoneConfig(srv.URL, `active = true`)},
			{
				Config:      zoneConfig(srv.URL, `active = true`),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?s)Cannot destroy an active zone.*active = false.*then destroy`),
			},
			// Two-step teardown: deactivate, then the final destroy passes.
			{Config: zoneConfig(srv.URL, ``)},
		},
	})
}

func TestZoneImport(t *testing.T) {
	_, srv := newFakeZoneAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: zoneConfig(srv.URL, `default_record_ttl = 3600`)},
			{
				ResourceName:      "comlaude_zone.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["comlaude_zone.test"]
					return fmt.Sprintf("g-1/d-1/%s", rs.Primary.ID), nil
				},
			},
		},
	})
}

func TestZoneCreatePaymentRequiredNamesEntitlement(t *testing.T) {
	fake, srv := newFakeZoneAPI(t)
	fake.setPaymentRequired(true)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      zoneConfig(srv.URL, ``),
				ExpectError: regexp.MustCompile(`(?s)Zone creation failed.*zone service entitlement`),
			},
		},
	})
}

func TestZoneExternallyDeletedIsRecreated(t *testing.T) {
	fake, srv := newFakeZoneAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: zoneConfig(srv.URL, ``)},
			{
				PreConfig: func() { fake.externalDeleteZones() },
				Config:    zoneConfig(srv.URL, ``),
				Check:     resource.TestCheckResourceAttrSet("comlaude_zone.test", "id"),
			},
		},
	})

	if got := fake.createCount(); got != 2 {
		t.Errorf("creates = %d, want 2 (externally deleted zone must be recreated)", got)
	}
}

func TestZoneSupplierAmbiguityListsChoices(t *testing.T) {
	fake, srv := newFakeZoneAPI(t)
	// A second unused DNS supplier makes the choice genuinely ambiguous.
	fake.addSupplier("s-4", "Backup DNS", "dns_supplier_backup")
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      zoneConfig(srv.URL, ``),
				ExpectError: regexp.MustCompile(`(?s)more than one DNS supplier.*Backup DNS.*dns_supplier_backup.*Com Laude DNS`),
			},
		},
	})
}

func TestZoneSupplierExplicitSelector(t *testing.T) {
	fake, srv := newFakeZoneAPI(t)
	fake.addSupplier("s-4", "Backup DNS", "dns_supplier_backup")
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: zoneConfig(srv.URL, `supplier = "Backup DNS"`),
				Check:  resource.TestCheckResourceAttr("comlaude_zone.test", "supplier_id", "s-4"),
			},
		},
	})
}

func TestZoneCreateIsInactiveByDefault(t *testing.T) {
	_, srv := newFakeZoneAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: zoneConfig(srv.URL, `default_record_ttl = 3600`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("comlaude_zone.test", "id"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "active", "false"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "default_record_ttl", "3600"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "signed", "false"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "supplier_id", "s-2"),
					resource.TestCheckResourceAttr("comlaude_zone.test", "networks.#", "2"),
				),
			},
		},
	})
}
