// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
// The factory function is called for each Terraform CLI command to create a provider
// server that the CLI can connect to and interact with.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"comlaude": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	// Acceptance tests only ever touch the designated test domain; refusing
	// to run without it is the harness's first safety rail.
	for _, v := range []string{"COMLAUDE_USERNAME", "COMLAUDE_PASSWORD", "COMLAUDE_API_KEY", "COMLAUDE_TEST_DOMAIN"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests (use 'make testacc')", v)
		}
	}
}
