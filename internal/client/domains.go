// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/url"
)

// Domain is a registered domain as returned by the domains endpoints. Read-
// only in v1: lifecycle mutations go through Domain Orders, out of scope.
type Domain struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	TLD              string      `json:"tld"`
	ManagementStatus string      `json:"management_status"`
	RegisteredAt     string      `json:"registered_at"`
	ExpiresAt        string      `json:"expires_at"`
	DNSSEC           bool        `json:"dnssec"`
	Nameservers      Nameservers `json:"nameservers"`
	Account          Account     `json:"account"`
	ActiveZone       *Zone       `json:"active_zone"`
}

// Nameservers is the domain's delegation set.
type Nameservers struct {
	Names  []string `json:"names"`
	Labels []string `json:"labels"`
}

// Account is the account (group) a domain lives in.
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Zone is a DNS zone. Embedded in Domain as active_zone; also returned by
// the zone endpoints.
type Zone struct {
	ID                  string    `json:"id"`
	Active              bool      `json:"active"`
	Signed              bool      `json:"signed"`
	DefaultRecordTTL    int64     `json:"default_record_ttl"`
	ResourceRecordCount int64     `json:"resource_record_count"`
	Networks            []int     `json:"networks"`
	Supplier            *Supplier `json:"supplier"`
}

// Supplier is the DNS platform a zone runs on. Zone creation requires a
// supplier id (live-verified; the OpenAPI spec wrongly marks it optional).
type Supplier struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

// FindDomainByName looks a domain up by exact name within a group. The API's
// filter may match loosely, so the result is post-filtered to the exact name;
// no exact match is ErrNotFound.
func (c *Client) FindDomainByName(ctx context.Context, groupID, name string) (Domain, error) {
	path := fmt.Sprintf("/groups/%s/domains?filter[name]=%s", url.PathEscape(groupID), url.QueryEscape(name))
	domains, err := many[Domain](ctx, c, "GET", path)
	if err != nil {
		return Domain{}, err
	}
	for _, d := range domains {
		if d.Name == name {
			return d, nil
		}
	}
	return Domain{}, fmt.Errorf("domain %q in group %s: %w", name, groupID, ErrNotFound)
}
