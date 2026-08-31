// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/echoprovider"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
// The factory function is called for each Terraform CLI command to create a provider
// server that the CLI can connect to and interact with.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"comlaude": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccProtoV6ProviderFactoriesWithEcho includes the echo provider alongside the comlaude provider.
// It allows for testing assertions on data returned by an ephemeral resource during Open.
// The echoprovider is used to arrange tests by echoing ephemeral data into the Terraform state.
// This lets the data be referenced in test assertions with state checks.
var testAccProtoV6ProviderFactoriesWithEcho = map[string]func() (tfprotov6.ProviderServer, error){
	"comlaude": providerserver.NewProtocol6WithError(New("test")()),
	"echo":     echoprovider.NewProviderServer(),
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
