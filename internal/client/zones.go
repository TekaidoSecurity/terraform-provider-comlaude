// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
)

// ZoneInput is the mutable surface of a zone: create accepts both fields,
// PATCH updates either in place. Activation carries the cross-zone side
// effect (it deactivates the domain's other zones), so it is deliberately
// explicit — a nil Active is simply not sent.
type ZoneInput struct {
	DefaultRecordTTL *int64
	Active           *bool
	// SupplierID is required by the API on create (live-verified).
	SupplierID string
}

func (in ZoneInput) form() url.Values {
	form := url.Values{}
	if in.SupplierID != "" {
		form.Set("supplier_id", in.SupplierID)
	}
	if in.DefaultRecordTTL != nil {
		form.Set("default_record_ttl", strconv.FormatInt(*in.DefaultRecordTTL, 10))
	}
	if in.Active != nil {
		if *in.Active {
			form.Set("active", "1")
		} else {
			form.Set("active", "0")
		}
	}
	return form
}

// guardActivation is the acceptance-harness safety rail: under TF_ACC no
// request may activate a zone — activating a test zone would deactivate the
// domain's live zone and break the records it serves. Fails before any HTTP.
func guardActivation(in ZoneInput) error {
	if in.Active != nil && *in.Active && os.Getenv("TF_ACC") == "1" {
		return fmt.Errorf("refusing to set active=1 while TF_ACC is set: " +
			"activating a zone deactivates the domain's other zones, which would break the live test zone; " +
			"the activation path is covered by mocked tests and the supervised pre-release check only")
	}
	return nil
}

func zonesPath(groupID, domainID string) string {
	return fmt.Sprintf("/groups/%s/domains/%s/zones", url.PathEscape(groupID), url.PathEscape(domainID))
}

// CreateZone creates a zone on a domain. Requires the zone-manager role; can
// return ErrPaymentRequired when the zone service is not enabled.
func (c *Client) CreateZone(ctx context.Context, groupID, domainID string, in ZoneInput) (Zone, error) {
	if err := guardActivation(in); err != nil {
		return Zone{}, err
	}
	raw, err := c.do(ctx, "POST", zonesPath(groupID, domainID), in.form())
	if err != nil {
		return Zone{}, err
	}
	var env Envelope[Zone]
	if err := unmarshalEnvelope(raw, &env); err != nil {
		return Zone{}, err
	}
	z, err := env.One()
	if err != nil {
		return Zone{}, fmt.Errorf("create zone response: %w", err)
	}
	return z, nil
}

// GetZone reads one zone.
func (c *Client) GetZone(ctx context.Context, groupID, domainID, zoneID string) (Zone, error) {
	return one[Zone](ctx, c, "GET", zonesPath(groupID, domainID)+"/"+url.PathEscape(zoneID))
}

// ListZones lists a domain's zones.
func (c *Client) ListZones(ctx context.Context, groupID, domainID string) ([]Zone, error) {
	return many[Zone](ctx, c, "GET", zonesPath(groupID, domainID))
}

// UpdateZone PATCHes a zone in place.
func (c *Client) UpdateZone(ctx context.Context, groupID, domainID, zoneID string, in ZoneInput) error {
	if err := guardActivation(in); err != nil {
		return err
	}
	_, err := c.do(ctx, "PATCH", zonesPath(groupID, domainID)+"/"+url.PathEscape(zoneID), in.form())
	return err
}

// DeleteZone deletes a zone. The API only permits this on inactive zones.
func (c *Client) DeleteZone(ctx context.Context, groupID, domainID, zoneID string) error {
	_, err := c.do(ctx, "DELETE", zonesPath(groupID, domainID)+"/"+url.PathEscape(zoneID), nil)
	return err
}
