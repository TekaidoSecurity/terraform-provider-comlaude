// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Supplier facts verified live (2026-08-31): DNS suppliers carry keys
// prefixed dns_supplier_; a domain may hold at most ONE zone per supplier,
// and zone create requires a supplier id despite the spec marking it
// optional.

const suppliersOK = `{"errors":[],"messages":[],"data":[
	{"id":"s-1","name":"NS1","key":"dns_supplier_ns1"},
	{"id":"s-2","name":"Com Laude DNS","key":"dns_supplier_comlaude"},
	{"id":"s-3","name":"Registrar X","key":"registrar_x"}
],"status_code":200}`

const oneZoneOnS1 = `{"errors":[],"messages":[],"data":[
	{"id":"z-live","active":true,"supplier":{"id":"s-1","name":"NS1","key":"dns_supplier_ns1"}}
],"status_code":200}`

func supplierTestServer(t *testing.T, suppliers, zones string) *Client {
	t.Helper()
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/suppliers":
			w.Write([]byte(suppliers))
		case strings.HasSuffix(r.URL.Path, "/zones"):
			w.Write([]byte(zones))
		default:
			t.Errorf("unexpected call: %s", r.URL.Path)
		}
	})
	t.Cleanup(srv.Close)
	return New(srv.URL, "u", "p", "k")
}

func TestResolveDNSSupplierAutoPicksTheOnlyUnused(t *testing.T) {
	c := supplierTestServer(t, suppliersOK, oneZoneOnS1)
	s, err := c.ResolveDNSSupplier(context.Background(), "g-1", "d-1", "")
	if err != nil {
		t.Fatalf("ResolveDNSSupplier: %v", err)
	}
	// s-1 is taken by the existing zone; s-3 is not a DNS supplier; only
	// s-2 remains — no ambiguity, no config needed.
	if s.ID != "s-2" {
		t.Errorf("picked %+v, want s-2 (Com Laude DNS)", s)
	}
}

func TestResolveDNSSupplierAmbiguityListsCandidates(t *testing.T) {
	noZones := `{"errors":[],"messages":[],"data":[],"status_code":200}`
	c := supplierTestServer(t, suppliersOK, noZones)
	_, err := c.ResolveDNSSupplier(context.Background(), "g-1", "d-1", "")
	var amb *AmbiguousSupplierError
	if !errors.As(err, &amb) {
		t.Fatalf("want AmbiguousSupplierError, got %v", err)
	}
	if len(amb.Candidates) != 2 {
		t.Errorf("candidates = %+v, want NS1 and Com Laude DNS", amb.Candidates)
	}
	for _, want := range []string{"NS1", "Com Laude DNS", "dns_supplier_ns1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name candidate %q, got: %v", want, err)
		}
	}
}

func TestResolveDNSSupplierBySelector(t *testing.T) {
	noZones := `{"errors":[],"messages":[],"data":[],"status_code":200}`
	for _, selector := range []string{"NS1", "dns_supplier_ns1", "s-1"} {
		c := supplierTestServer(t, suppliersOK, noZones)
		s, err := c.ResolveDNSSupplier(context.Background(), "g-1", "d-1", selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if s.ID != "s-1" {
			t.Errorf("selector %q picked %+v, want s-1", selector, s)
		}
	}
}

func TestResolveDNSSupplierSelectorAlreadyUsed(t *testing.T) {
	c := supplierTestServer(t, suppliersOK, oneZoneOnS1)
	_, err := c.ResolveDNSSupplier(context.Background(), "g-1", "d-1", "NS1")
	if err == nil || !strings.Contains(err.Error(), "already has a zone") {
		t.Fatalf("selecting a used supplier must be pre-empted with a clear error, got %v", err)
	}
}

func TestResolveDNSSupplierUnknownSelectorListsOptions(t *testing.T) {
	noZones := `{"errors":[],"messages":[],"data":[],"status_code":200}`
	c := supplierTestServer(t, suppliersOK, noZones)
	_, err := c.ResolveDNSSupplier(context.Background(), "g-1", "d-1", "Cloudflare")
	if err == nil || !strings.Contains(err.Error(), "Com Laude DNS") {
		t.Fatalf("unknown selector error must list available DNS suppliers, got %v", err)
	}
}
