// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// Fixture shaped from the live GET /groups/{gid}/domains capture
// (docs/research/comlaude-api-summary.md; UUIDs randomized).
const domainListOK = `{
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
			"account": {"id": "aaaa1111-0000-0000-0000-000000000000", "name": "KERING - Testing", "parent": {"id": "bbbb2222-0000-0000-0000-000000000000", "name": "KERING"}},
			"active_zone": {"id": "cccc3333-0000-0000-0000-000000000000", "default_record_ttl": 86400, "signed": false, "resource_record_count": 22, "networks": [0, 9002]}
		}
	],
	"status_code": 200
}`

const domainListEmpty = `{"errors": [], "messages": [], "data": [], "status_code": 200}`

func TestFindDomainByNameReturnsMatchWithEmbeds(t *testing.T) {
	var gotQuery string
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/groups/g-1/domains" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("filter[name]")
		w.Write([]byte(domainListOK))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	d, err := c.FindDomainByName(context.Background(), "g-1", "example-lab.com")
	if err != nil {
		t.Fatalf("FindDomainByName: %v", err)
	}
	if gotQuery != "example-lab.com" {
		t.Errorf("filter[name] = %q", gotQuery)
	}
	if d.ID != "11111111-2222-3333-4444-555555555555" || d.TLD != "com" {
		t.Errorf("unexpected domain: %+v", d)
	}
	if d.Account.Name != "KERING - Testing" {
		t.Errorf("account = %+v", d.Account)
	}
	if d.ActiveZone == nil || d.ActiveZone.ID != "cccc3333-0000-0000-0000-000000000000" ||
		d.ActiveZone.DefaultRecordTTL != 86400 || d.ActiveZone.ResourceRecordCount != 22 {
		t.Errorf("active_zone = %+v", d.ActiveZone)
	}
	if len(d.Nameservers.Names) != 2 || d.Nameservers.Names[0] != "dns1.comlaude-dns.com" {
		t.Errorf("nameservers = %+v", d.Nameservers)
	}
}

func TestFindDomainByNameNotFound(t *testing.T) {
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(domainListEmpty))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	_, err := c.FindDomainByName(context.Background(), "g-1", "missing.com")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFindDomainByNameIgnoresFuzzyMatches(t *testing.T) {
	// The API's filter may substring-match; the client must return only the
	// exact name asked for.
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(domainListOK)) // contains example-lab.com only
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	_, err := c.FindDomainByName(context.Background(), "g-1", "lab.com")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for fuzzy result, got %v", err)
	}
}
