// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const mockDomainList = `{
	"errors": [],
	"messages": [],
	"data": [
		{
			"id": "11111111-2222-3333-4444-555555555555",
			"name": "example-lab.com",
			"tld": "com",
			"management_status": "auto_renew_enabled",
			"registered_at": "2020-01-15T00:00:00Z",
			"expires_at": "2026-12-04T00:00:00Z",
			"dnssec": false,
			"nameservers": {"names": ["dns1.comlaude-dns.com", "dns2.comlaude-dns.net"], "labels": ["com_laude_dns"]},
			"account": {"id": "aaaa1111-0000-0000-0000-000000000000", "name": "KERING - Testing"},
			"active_zone": {"id": "cccc3333-0000-0000-0000-000000000000", "default_record_ttl": 86400, "signed": false, "resource_record_count": 22, "networks": [0, 9002]}
		}
	],
	"status_code": 200
}`

const mockDomainListEmpty = `{"errors": [], "messages": [], "data": [], "status_code": 200}`

// newDomainAPI extends the Configure mock with the domains route; it records
// which group each domains request was scoped to.
func newDomainAPI(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var domainGroups []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api_login":
			w.Write([]byte(mockLoginOK))
		case r.URL.Path == "/profile":
			w.Write([]byte(mockProfileOK))
		case strings.HasSuffix(r.URL.Path, "/domains"):
			parts := strings.Split(r.URL.Path, "/") // /groups/{gid}/domains
			domainGroups = append(domainGroups, parts[2])
			if r.URL.Query().Get("filter[name]") == "example-lab.com" {
				w.Write([]byte(mockDomainList))
				return
			}
			w.Write([]byte(mockDomainListEmpty))
		default:
			t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &domainGroups
}

func TestDomainDataSourceExportsSpecAttributes(t *testing.T) {
	srv, _ := newDomainAPI(t)
	setCredEnv(t)

	config := fmt.Sprintf(`
provider "comlaude" {
  base_url = %q
}
data "comlaude_domain" "main" {
  name = "example-lab.com"
}
`, srv.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "id", "11111111-2222-3333-4444-555555555555"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "account_id", "aaaa1111-0000-0000-0000-000000000000"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "account_name", "KERING - Testing"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "management_status", "auto_renew_enabled"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "registered_at", "2020-01-15T00:00:00Z"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "expires_at", "2026-12-04T00:00:00Z"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "tld", "com"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "dnssec", "false"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "nameservers.#", "2"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "nameservers.0", "dns1.comlaude-dns.com"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "active_zone_id", "cccc3333-0000-0000-0000-000000000000"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "active_zone_ttl", "86400"),
					resource.TestCheckResourceAttr("data.comlaude_domain.main", "active_zone_record_count", "22"),
				),
			},
		},
	})
}

func TestDomainDataSourceGroupOverride(t *testing.T) {
	srv, domainGroups := newDomainAPI(t)
	setCredEnv(t)

	config := fmt.Sprintf(`
provider "comlaude" {
  base_url = %q
  group_id = "group-provider-level"
}
data "comlaude_domain" "main" {
  name     = "example-lab.com"
  group_id = "group-override"
}
`, srv.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
		},
	})

	for _, g := range *domainGroups {
		if g != "group-override" {
			t.Errorf("domains request scoped to group %q, want group-override", g)
		}
	}
	if len(*domainGroups) == 0 {
		t.Error("no domains request observed")
	}
}

func TestDomainDataSourceNotFoundIsError(t *testing.T) {
	srv, _ := newDomainAPI(t)
	setCredEnv(t)

	config := fmt.Sprintf(`
provider "comlaude" {
  base_url = %q
}
data "comlaude_domain" "main" {
  name = "missing.com"
}
`, srv.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)missing\.com.*not found`),
			},
		},
	})
}
