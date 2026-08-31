// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// These tests exercise provider Configure through a real Terraform plan via
// the comlaude_domain data source. The mock server stands in for the API.

const mockLoginOK = `{"errors":[],"messages":[],"data":{"token_type":"Bearer","expires_in":7200,"access_token":"test-jwt","refresh_token":"r"},"status_code":200}`
const mockLoginBad = `{"errors":[{"code":"invalid_credentials","message":"These credentials do not match our records"}],"messages":[],"data":[],"status_code":401}`
const mockProfileOK = `{"errors":[],"messages":[],"data":{"id":"u-1","group_id":"group-from-profile","name":"svc"},"status_code":200}`

type apiCounters struct {
	logins   atomic.Int32
	profiles atomic.Int32
}

func newMockAPI(t *testing.T, badCreds bool) (*httptest.Server, *apiCounters) {
	t.Helper()
	counters := &apiCounters{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api_login":
			counters.logins.Add(1)
			if badCreds {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(mockLoginBad))
				return
			}
			w.Write([]byte(mockLoginOK))
		case r.URL.Path == "/profile":
			counters.profiles.Add(1)
			w.Write([]byte(mockProfileOK))
		case strings.HasSuffix(r.URL.Path, "/domains"):
			w.Write([]byte(mockDomainList))
		default:
			t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, counters
}

func configWithDataSource(baseURL, extraProviderAttrs string) string {
	return fmt.Sprintf(`
provider "comlaude" {
  base_url = %q
  %s
}

data "comlaude_domain" "probe" {
  name = "example-lab.com"
}
`, baseURL, extraProviderAttrs)
}

func setCredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("COMLAUDE_USERNAME", "svc@example.com")
	t.Setenv("COMLAUDE_PASSWORD", "secret pw")
	t.Setenv("COMLAUDE_API_KEY", "key-1")
	t.Setenv("COMLAUDE_GROUP_ID", "")
	t.Setenv("COMLAUDE_BASE_URL", "")
}

func TestConfigureLogsInEagerlyAndFallsBackToProfileGroup(t *testing.T) {
	srv, counters := newMockAPI(t, false)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: configWithDataSource(srv.URL, "")},
		},
	})

	if got := counters.logins.Load(); got < 1 {
		t.Errorf("logins = %d, want at least 1 (eager login at Configure)", got)
	}
	if got := counters.profiles.Load(); got < 1 {
		t.Errorf("profile calls = %d, want at least 1 (group_id fallback)", got)
	}
}

func TestConfigureExplicitGroupSkipsProfileLookup(t *testing.T) {
	srv, counters := newMockAPI(t, false)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: configWithDataSource(srv.URL, `group_id = "group-explicit"`)},
		},
	})

	if got := counters.profiles.Load(); got != 0 {
		t.Errorf("profile calls = %d, want 0 when group_id is explicit", got)
	}
}

func TestConfigureGroupEnvSkipsProfileLookup(t *testing.T) {
	srv, counters := newMockAPI(t, false)
	setCredEnv(t)
	t.Setenv("COMLAUDE_GROUP_ID", "group-from-env")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: configWithDataSource(srv.URL, "")},
		},
	})

	if got := counters.profiles.Load(); got != 0 {
		t.Errorf("profile calls = %d, want 0 when COMLAUDE_GROUP_ID is set", got)
	}
}

func TestConfigureBadCredentialsFailsFast(t *testing.T) {
	srv, _ := newMockAPI(t, true)
	setCredEnv(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      configWithDataSource(srv.URL, ""),
				ExpectError: regexp.MustCompile(`(?s)credentials.*do not match our records`),
			},
		},
	})
}

func TestConfigureMissingCredentialsIsActionable(t *testing.T) {
	t.Setenv("COMLAUDE_USERNAME", "")
	t.Setenv("COMLAUDE_PASSWORD", "")
	t.Setenv("COMLAUDE_API_KEY", "")
	t.Setenv("COMLAUDE_GROUP_ID", "")
	t.Setenv("COMLAUDE_BASE_URL", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      configWithDataSource("http://127.0.0.1:1", ""),
				ExpectError: regexp.MustCompile(`COMLAUDE_USERNAME`),
			},
		},
	})
}
