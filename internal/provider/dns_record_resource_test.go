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

func recordConfig(baseURL, zoneID, name, typ string, ttl int, value, extra string) string {
	return fmt.Sprintf(`
provider "comlaude" {
  base_url = %q
  group_id = "g-1"
}
resource "comlaude_dns_record" "test" {
  zone_id = %q
  name    = %q
  type    = %q
  ttl     = %d
  value   = %q
  %s
}
`, baseURL, zoneID, name, typ, ttl, value, extra)
}

func TestDnsRecordApexUsesAtSign(t *testing.T) {
	_, srv := newFakeRecordAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: recordConfig(srv.URL, "z-1", "@", "TXT", 300, "v=spf1 -all", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "name", "@"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "fqdn", "example-lab.com"),
				),
			},
		},
	})
}

func TestDnsRecordUpdatesInPlace(t *testing.T) {
	fake, srv := newFakeRecordAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: recordConfig(srv.URL, "z-1", "www", "A", 300, "1.2.3.4", "")},
			{
				// name, ttl AND value all change: still an in-place PUT.
				Config: recordConfig(srv.URL, "z-1", "www2", "A", 600, "5.6.7.8", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "fqdn", "www2.example-lab.com"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "value", "5.6.7.8"),
				),
			},
		},
	})

	if got := fake.createCount(); got != 1 {
		t.Errorf("creates = %d, want 1 (updates must be in-place, not delete+create)", got)
	}
}

func TestDnsRecordZoneChangeForcesReplacement(t *testing.T) {
	fake, srv := newFakeRecordAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: recordConfig(srv.URL, "z-1", "www", "A", 300, "1.2.3.4", "")},
			{
				Config: recordConfig(srv.URL, "z-2", "www", "A", 300, "1.2.3.4", ""),
				Check:  resource.TestCheckResourceAttr("comlaude_dns_record.test", "fqdn", "www.second-lab.com"),
			},
		},
	})

	if got := fake.createCount(); got != 2 {
		t.Errorf("creates = %d, want 2 (zone change must replace)", got)
	}
}

func TestDnsRecordImport(t *testing.T) {
	_, srv := newFakeRecordAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: recordConfig(srv.URL, "z-1", "www", "A", 300, "1.2.3.4", "")},
			{
				ResourceName:      "comlaude_dns_record.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["comlaude_dns_record.test"]
					return fmt.Sprintf("g-1/z-1/%s", rs.Primary.ID), nil
				},
			},
		},
	})
}

func TestDnsRecordRejectsTypeForeignAttributes(t *testing.T) {
	_, srv := newFakeRecordAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      recordConfig(srv.URL, "z-1", "www", "A", 300, "1.2.3.4", "priority = 10"),
				ExpectError: regexp.MustCompile(`(?s)priority.*cannot be set on a A record`),
			},
		},
	})
}

func TestDnsRecordExternallyDeletedIsRecreated(t *testing.T) {
	fake, srv := newFakeRecordAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: recordConfig(srv.URL, "z-1", "www", "A", 300, "1.2.3.4", "")},
			{
				PreConfig: func() { fake.externalDelete("z-1") },
				Config:    recordConfig(srv.URL, "z-1", "www", "A", 300, "1.2.3.4", ""),
				Check:     resource.TestCheckResourceAttrSet("comlaude_dns_record.test", "id"),
			},
		},
	})

	if got := fake.createCount(); got != 2 {
		t.Errorf("creates = %d, want 2 (externally deleted record must be recreated, not errored)", got)
	}
}

func TestDnsRecordCreateTranslatesRelativeName(t *testing.T) {
	_, srv := newFakeRecordAPI(t)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: recordConfig(srv.URL, "z-1", "www", "A", 300, "1.2.3.4", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("comlaude_dns_record.test", "id"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "name", "www"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "fqdn", "www.example-lab.com"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "locked", "false"),
					resource.TestCheckResourceAttr("comlaude_dns_record.test", "group_id", "g-1"),
				),
			},
		},
	})
}
