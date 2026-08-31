// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// The API's zone object has no domain back-reference, so the client resolves
// a zone's domain name itself: from any record in the zone, else from the
// group's domains (whose payloads embed active_zone), else by walking each
// domain's zones. The result is cached per zone.

func TestResolveZoneDomainFromExistingRecord(t *testing.T) {
	var calls atomic.Int32
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.HasSuffix(r.URL.Path, "/zones/z-1/records") {
			w.Write([]byte(recordListPage)) // holds a record with zone.domain.name
			return
		}
		t.Errorf("unexpected call: %s", r.URL.Path)
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	name, err := c.ResolveZoneDomain(context.Background(), "g-1", "z-1")
	if err != nil {
		t.Fatalf("ResolveZoneDomain: %v", err)
	}
	if name != "example-lab.com" {
		t.Errorf("domain = %q", name)
	}

	// Second call answers from the cache: no further API traffic.
	before := calls.Load()
	if _, err := c.ResolveZoneDomain(context.Background(), "g-1", "z-1"); err != nil {
		t.Fatalf("cached resolve: %v", err)
	}
	if calls.Load() != before {
		t.Errorf("cache miss: %d extra calls", calls.Load()-before)
	}
}

func TestResolveZoneDomainForEmptyActiveZoneViaDomainList(t *testing.T) {
	emptyRecords := `{"errors":[],"messages":[],"data":[],"status_code":200,"meta":{"pagination":{"total":0,"count":0,"per_page":25,"current_page":1,"total_pages":1}}}`
	domains := `{"errors":[],"messages":[],"data":[
		{"id":"d-1","name":"other.com","active_zone":{"id":"z-other"}},
		{"id":"d-2","name":"example-lab.com","active_zone":{"id":"z-empty"}}
	],"status_code":200,"meta":{"pagination":{"total":2,"count":2,"per_page":1000,"current_page":1,"total_pages":1}}}`

	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/zones/z-empty/records"):
			w.Write([]byte(emptyRecords))
		case strings.HasSuffix(r.URL.Path, "/g-1/domains"):
			w.Write([]byte(domains))
		default:
			t.Errorf("unexpected call: %s", r.URL.Path)
		}
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	name, err := c.ResolveZoneDomain(context.Background(), "g-1", "z-empty")
	if err != nil {
		t.Fatalf("ResolveZoneDomain: %v", err)
	}
	if name != "example-lab.com" {
		t.Errorf("domain = %q", name)
	}
}

func TestResolveZoneDomainForEmptyInactiveZoneWalksDomainZones(t *testing.T) {
	emptyRecords := `{"errors":[],"messages":[],"data":[],"status_code":200,"meta":{"pagination":{"total":0,"count":0,"per_page":25,"current_page":1,"total_pages":1}}}`
	domains := `{"errors":[],"messages":[],"data":[
		{"id":"d-1","name":"other.com","active_zone":{"id":"z-other"}},
		{"id":"d-2","name":"example-lab.com","active_zone":{"id":"z-live"}}
	],"status_code":200,"meta":{"pagination":{"total":2,"count":2,"per_page":1000,"current_page":1,"total_pages":1}}}`
	zonesOther := `{"errors":[],"messages":[],"data":[{"id":"z-other","active":true}],"status_code":200}`
	zonesLab := `{"errors":[],"messages":[],"data":[{"id":"z-live","active":true},{"id":"z-staging","active":false}],"status_code":200}`

	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/zones/z-staging/records"):
			w.Write([]byte(emptyRecords))
		case strings.HasSuffix(r.URL.Path, "/g-1/domains"):
			w.Write([]byte(domains))
		case strings.HasSuffix(r.URL.Path, "/domains/d-1/zones"):
			w.Write([]byte(zonesOther))
		case strings.HasSuffix(r.URL.Path, "/domains/d-2/zones"):
			w.Write([]byte(zonesLab))
		default:
			t.Errorf("unexpected call: %s", r.URL.Path)
		}
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	name, err := c.ResolveZoneDomain(context.Background(), "g-1", "z-staging")
	if err != nil {
		t.Fatalf("ResolveZoneDomain: %v", err)
	}
	if name != "example-lab.com" {
		t.Errorf("domain = %q", name)
	}
}
