// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

const zoneViewOK = `{
	"errors": [], "messages": [],
	"data": {"id": "z-9", "active": false, "signed": false, "default_record_ttl": 3600, "networks": [0, 9002]},
	"status_code": 200
}`

func TestCreateZoneSendsFormAndParsesZone(t *testing.T) {
	var gotForm map[string][]string
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/groups/g-1/domains/d-1/zones" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		r.ParseForm()
		gotForm = r.PostForm
		w.Write([]byte(zoneViewOK))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	ttl := int64(3600)
	z, err := c.CreateZone(context.Background(), "g-1", "d-1", ZoneInput{DefaultRecordTTL: &ttl})
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if z.ID != "z-9" || z.DefaultRecordTTL != 3600 || z.Active {
		t.Errorf("unexpected zone: %+v", z)
	}
	if got := gotForm["default_record_ttl"]; len(got) != 1 || got[0] != "3600" {
		t.Errorf("form[default_record_ttl] = %v", got)
	}
	if _, present := gotForm["active"]; present {
		t.Error("active must not be sent when unset")
	}
}

func TestGetUpdateDeleteZone(t *testing.T) {
	var gotMethod, gotPath string
	var gotForm map[string][]string
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		r.ParseForm()
		gotForm = r.PostForm
		w.Write([]byte(zoneViewOK))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	z, err := c.GetZone(context.Background(), "g-1", "d-1", "z-9")
	if err != nil {
		t.Fatalf("GetZone: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/groups/g-1/domains/d-1/zones/z-9" {
		t.Errorf("get went to %s %s", gotMethod, gotPath)
	}
	if z.DefaultRecordTTL != 3600 {
		t.Errorf("zone = %+v", z)
	}

	ttl := int64(7200)
	active := true
	if err := c.UpdateZone(context.Background(), "g-1", "d-1", "z-9", ZoneInput{DefaultRecordTTL: &ttl, Active: &active}); err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("update method = %s, want PATCH", gotMethod)
	}
	if gotForm["default_record_ttl"][0] != "7200" || gotForm["active"][0] != "1" {
		t.Errorf("update form = %v", gotForm)
	}

	if err := c.DeleteZone(context.Background(), "g-1", "d-1", "z-9"); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/groups/g-1/domains/d-1/zones/z-9" {
		t.Errorf("delete went to %s %s", gotMethod, gotPath)
	}
}

func TestZoneActivationRefusedUnderTFACC(t *testing.T) {
	// The harness safety rail: with TF_ACC set, no request carrying active=1
	// may ever leave the process — activating a test zone would deactivate
	// the live zone serving the test domain.
	var calls atomic.Int32
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(zoneViewOK))
	})
	defer srv.Close()

	t.Setenv("TF_ACC", "1")
	c := New(srv.URL, "u", "p", "k")
	active := true

	_, err := c.CreateZone(context.Background(), "g-1", "d-1", ZoneInput{Active: &active})
	if err == nil || !strings.Contains(err.Error(), "TF_ACC") {
		t.Fatalf("create with active=1 under TF_ACC must be refused, got %v", err)
	}
	if err := c.UpdateZone(context.Background(), "g-1", "d-1", "z-9", ZoneInput{Active: &active}); err == nil || !strings.Contains(err.Error(), "TF_ACC") {
		t.Fatalf("update with active=1 under TF_ACC must be refused, got %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("API calls = %d, want 0 (guard must fire before any HTTP)", calls.Load())
	}
}

func TestCreateZonePaymentRequired(t *testing.T) {
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"status_code":402,"data":[],"messages":[],"errors":[{"code":"payment_required","message":"Zone service is not enabled for this account"}]}`))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	_, err := c.CreateZone(context.Background(), "g-1", "d-1", ZoneInput{})
	if !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("want ErrPaymentRequired, got %v", err)
	}
}
