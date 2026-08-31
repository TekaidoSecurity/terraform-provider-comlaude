// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/url"
)

// ResolveZoneDomain returns the domain name a zone belongs to. The API's
// zone object carries no domain back-reference, so resolution is layered:
//  1. any record in the zone embeds zone.domain.name (one limit=1 list);
//  2. an empty active zone is found via the group's domain list, whose
//     payloads embed active_zone;
//  3. an empty inactive zone is found by walking each domain's zone list.
//
// Results are cached per (group, zone) for the process lifetime: a zone
// cannot move to another domain.
func (c *Client) ResolveZoneDomain(ctx context.Context, groupID, zoneID string) (string, error) {
	cacheKey := groupID + "/" + zoneID
	c.mu.Lock()
	if name, ok := c.zoneDomains[cacheKey]; ok {
		c.mu.Unlock()
		return name, nil
	}
	c.mu.Unlock()

	name, err := c.resolveZoneDomain(ctx, groupID, zoneID)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.zoneDomains[cacheKey] = name
	c.mu.Unlock()
	return name, nil
}

func (c *Client) resolveZoneDomain(ctx context.Context, groupID, zoneID string) (string, error) {
	// 1. Any record in the zone names its domain.
	records, err := many[Record](ctx, c, "GET", recordsPath(groupID, zoneID)+"?limit=1&page=1")
	if err != nil {
		return "", err
	}
	if len(records) > 0 && records[0].Zone.Domain.Name != "" {
		return records[0].Zone.Domain.Name, nil
	}

	// 2. Domain payloads embed active_zone.
	domainsPath := fmt.Sprintf("/groups/%s/domains?limit=1000", url.PathEscape(groupID))
	domains, err := many[Domain](ctx, c, "GET", domainsPath)
	if err != nil {
		return "", err
	}
	for _, d := range domains {
		if d.ActiveZone != nil && d.ActiveZone.ID == zoneID {
			return d.Name, nil
		}
	}

	// 3. Walk each domain's zones (rare: empty inactive zones only).
	for _, d := range domains {
		zonesPath := fmt.Sprintf("/groups/%s/domains/%s/zones", url.PathEscape(groupID), url.PathEscape(d.ID))
		zones, err := many[Zone](ctx, c, "GET", zonesPath)
		if err != nil {
			return "", err
		}
		for _, z := range zones {
			if z.ID == zoneID {
				return d.Name, nil
			}
		}
	}
	return "", fmt.Errorf("domain for zone %s in group %s: %w", zoneID, groupID, ErrNotFound)
}
