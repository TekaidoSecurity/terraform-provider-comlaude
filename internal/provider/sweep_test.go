// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/TekaidoSecurity/terraform-provider-comlaude/internal/client"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Sweepers purge leftovers from failed acceptance runs. Identification is
// purely by the tfacc- name prefix, and only within the designated test
// domain: nothing else is ever touched. Run with:
//
//	go test ./internal/provider/ -sweep=all
func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func sweeperClient() (*client.Client, string, client.Domain, error) {
	for _, v := range []string{"COMLAUDE_USERNAME", "COMLAUDE_PASSWORD", "COMLAUDE_API_KEY", "COMLAUDE_TEST_DOMAIN"} {
		if os.Getenv(v) == "" {
			return nil, "", client.Domain{}, fmt.Errorf("%s must be set to sweep", v)
		}
	}
	base := os.Getenv("COMLAUDE_BASE_URL")
	if base == "" {
		base = client.DefaultBaseURL
	}
	c := client.New(base, os.Getenv("COMLAUDE_USERNAME"), os.Getenv("COMLAUDE_PASSWORD"), os.Getenv("COMLAUDE_API_KEY"))

	groupID := os.Getenv("COMLAUDE_GROUP_ID")
	if groupID == "" {
		profile, err := c.GetProfile(context.Background())
		if err != nil {
			return nil, "", client.Domain{}, err
		}
		groupID = profile.GroupID
	}
	domain, err := c.FindDomainByName(context.Background(), groupID, os.Getenv("COMLAUDE_TEST_DOMAIN"))
	if err != nil {
		return nil, "", client.Domain{}, err
	}
	return c, groupID, domain, nil
}

func init() {
	resource.AddTestSweepers("comlaude_zone", &resource.Sweeper{
		Name: "comlaude_zone",
		F: func(_ string) error {
			c, groupID, domain, err := sweeperClient()
			if err != nil {
				return err
			}
			ctx := context.Background()
			zones, err := c.ListZones(ctx, groupID, domain.ID)
			if err != nil {
				return err
			}
			for _, z := range zones {
				// Never the active zone, and only zones whose every record
				// is tfacc- (an empty zone qualifies): anything else is not
				// ours to delete.
				if z.Active {
					continue
				}
				records, err := c.ListRecords(ctx, groupID, z.ID)
				if err != nil {
					return err
				}
				ours := true
				for _, rec := range records {
					if !strings.HasPrefix(rec.Name, "tfacc-") {
						ours = false
						break
					}
				}
				if !ours {
					continue
				}
				if err := c.DeleteZone(ctx, groupID, domain.ID, z.ID); err != nil {
					return fmt.Errorf("sweeping zone %s: %w", z.ID, err)
				}
				fmt.Printf("swept zone %s\n", z.ID)
			}
			return nil
		},
	})

	resource.AddTestSweepers("comlaude_dns_record", &resource.Sweeper{
		Name: "comlaude_dns_record",
		F: func(_ string) error {
			c, groupID, domain, err := sweeperClient()
			if err != nil {
				return err
			}
			if domain.ActiveZone == nil {
				return nil
			}
			ctx := context.Background()
			records, err := c.ListRecords(ctx, groupID, domain.ActiveZone.ID)
			if err != nil {
				return err
			}
			for _, rec := range records {
				if strings.HasPrefix(rec.Name, "tfacc-") {
					if err := c.DeleteRecord(ctx, groupID, domain.ActiveZone.ID, rec.ID); err != nil {
						return fmt.Errorf("sweeping record %s (%s): %w", rec.Name, rec.ID, err)
					}
					fmt.Printf("swept record %s (%s)\n", rec.Name, rec.ID)
				}
			}
			return nil
		},
	})
}
