// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Record is a Resource Record as returned by the list endpoint. Names are
// FQDNs on the wire (create rejects relative names — verified live); the
// embedded zone carries the domain back-reference the bare zone object lacks.
type Record struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Type     string     `json:"type"`
	TTL      int64      `json:"ttl"`
	Value    string     `json:"value"`
	Locked   int        `json:"locked"`
	Priority *int64     `json:"priority"`
	Weight   *int64     `json:"weight"`
	Port     *int64     `json:"port"`
	Flags    *int64     `json:"flags"`
	Tag      string     `json:"tag"`
	Digest   *int64     `json:"digest_type"`
	KeyTag   *int64     `json:"key_tag"`
	Algo     *int64     `json:"algorithm"`
	Zone     RecordZone `json:"zone"`
}

// RecordZone is the zone context embedded in a record read.
type RecordZone struct {
	ID     string    `json:"id"`
	Active bool      `json:"active"`
	Domain DomainRef `json:"domain"`
}

// DomainRef is the minimal domain reference embedded in a record's zone.
type DomainRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RecordInput is the mutable surface of a record for create and full-replace
// update. Name must be an FQDN ending in the zone's domain name.
type RecordInput struct {
	Name  string
	Type  string
	TTL   int64
	Value string

	Priority *int64 // MX, MXDUMMY, SRV
	Weight   *int64 // SRV
	Port     *int64 // SRV
	Flags    *int64 // CAA
	Tag      string // CAA
	Digest   *int64 // DS
	KeyTag   *int64 // DS
	Algo     *int64 // DS
}

func (in RecordInput) form() url.Values {
	form := url.Values{
		"name":  {in.Name},
		"type":  {in.Type},
		"ttl":   {strconv.FormatInt(in.TTL, 10)},
		"value": {in.Value},
	}
	for key, v := range map[string]*int64{
		"priority": in.Priority, "weight": in.Weight, "port": in.Port,
		"flags": in.Flags, "digest_type": in.Digest, "key_tag": in.KeyTag, "algorithm": in.Algo,
	} {
		if v != nil {
			form.Set(key, strconv.FormatInt(*v, 10))
		}
	}
	if in.Tag != "" {
		form.Set("tag", in.Tag)
	}
	return form
}

func recordsPath(groupID, zoneID string) string {
	return fmt.Sprintf("/groups/%s/zones/%s/records", url.PathEscape(groupID), url.PathEscape(zoneID))
}

// CreateRecord creates a record and returns its id. The API returns only
// {id} on create (verified live); callers Read to hydrate the full record.
func (c *Client) CreateRecord(ctx context.Context, groupID, zoneID string, in RecordInput) (string, error) {
	raw, err := c.do(ctx, "POST", recordsPath(groupID, zoneID), in.form())
	if err != nil {
		return "", err
	}
	var env Envelope[struct {
		ID string `json:"id"`
	}]
	if err := unmarshalEnvelope(raw, &env); err != nil {
		return "", err
	}
	created, err := env.One()
	if err != nil {
		return "", fmt.Errorf("create record response: %w", err)
	}
	return created.ID, nil
}

// GetRecord finds one record by id. The API has no single-record GET (405
// live), so this paginates the zone's record list.
func (c *Client) GetRecord(ctx context.Context, groupID, zoneID, recordID string) (Record, error) {
	base := recordsPath(groupID, zoneID)
	for page := 1; ; page++ {
		raw, err := c.do(ctx, "GET", fmt.Sprintf("%s?limit=1000&page=%d", base, page), nil)
		if err != nil {
			return Record{}, err
		}
		var env Envelope[Record]
		if err := unmarshalEnvelope(raw, &env); err != nil {
			return Record{}, err
		}
		records, err := env.Many()
		if err != nil {
			return Record{}, err
		}
		for _, r := range records {
			if r.ID == recordID {
				return r, nil
			}
		}
		if env.Meta == nil || env.Meta.Pagination == nil || page >= env.Meta.Pagination.TotalPages {
			return Record{}, fmt.Errorf("record %s in zone %s: %w", recordID, zoneID, ErrNotFound)
		}
	}
}

// ListRecords returns every record in a zone, paginating to exhaustion.
func (c *Client) ListRecords(ctx context.Context, groupID, zoneID string) ([]Record, error) {
	base := recordsPath(groupID, zoneID)
	var all []Record
	for page := 1; ; page++ {
		raw, err := c.do(ctx, "GET", fmt.Sprintf("%s?limit=1000&page=%d", base, page), nil)
		if err != nil {
			return nil, err
		}
		var env Envelope[Record]
		if err := unmarshalEnvelope(raw, &env); err != nil {
			return nil, err
		}
		records, err := env.Many()
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
		if env.Meta == nil || env.Meta.Pagination == nil || page >= env.Meta.Pagination.TotalPages {
			return all, nil
		}
	}
}

// UpdateRecord full-replaces a record in place (PUT keeps the id).
func (c *Client) UpdateRecord(ctx context.Context, groupID, zoneID, recordID string, in RecordInput) error {
	_, err := c.do(ctx, "PUT", recordsPath(groupID, zoneID)+"/"+url.PathEscape(recordID), in.form())
	return err
}

// DeleteRecord deletes a record.
func (c *Client) DeleteRecord(ctx context.Context, groupID, zoneID, recordID string) error {
	_, err := c.do(ctx, "DELETE", recordsPath(groupID, zoneID)+"/"+url.PathEscape(recordID), nil)
	return err
}
