// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Wire shapes below were captured live during the tfacc-probe (2026-08-31):
// create returns ONLY {id}; the full record comes from the list endpoint,
// whose items embed zone.domain.name.

const recordCreateOK = `{"errors":[],"messages":[],"data":{"id":"rec-1"},"status_code":200}`

const recordListPage = `{
	"errors": [], "messages": [],
	"data": [
		{
			"id": "rec-1",
			"type": "TXT",
			"name": "tfacc-probe.example-lab.com",
			"locked": 0,
			"ttl": 300,
			"value": "hello",
			"zone": {
				"id": "z-1", "default_record_ttl": 86400, "signed": false, "active": true,
				"domain": {"id": "d-1", "name": "example-lab.com"}
			}
		}
	],
	"status_code": 200,
	"meta": {"pagination": {"total": 1, "count": 1, "per_page": 25, "current_page": 1, "total_pages": 1}}
}`

func TestCreateRecordSendsFormAndReturnsID(t *testing.T) {
	var gotForm map[string][]string
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/groups/g-1/zones/z-1/records" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		r.ParseForm()
		gotForm = r.PostForm
		w.Write([]byte(recordCreateOK))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	prio := int64(10)
	weight := int64(5)
	port := int64(443)
	id, err := c.CreateRecord(context.Background(), "g-1", "z-1", RecordInput{
		Name: "_sip._tcp.example-lab.com", Type: "SRV", TTL: 3600, Value: "sip.example-lab.com",
		Priority: &prio, Weight: &weight, Port: &port,
	})
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if id != "rec-1" {
		t.Errorf("id = %q", id)
	}
	for k, want := range map[string]string{
		"name": "_sip._tcp.example-lab.com", "type": "SRV", "ttl": "3600",
		"value": "sip.example-lab.com", "priority": "10", "weight": "5", "port": "443",
	} {
		if got := gotForm[k]; len(got) != 1 || got[0] != want {
			t.Errorf("form[%s] = %v, want %q", k, got, want)
		}
	}
	if _, present := gotForm["flags"]; present {
		t.Error("unset type-specific fields must not be sent")
	}
}

func TestGetRecordFindsByIDAcrossPages(t *testing.T) {
	// Page 1 holds other records; the target is on page 2. No single-record
	// GET exists (405 live), so Read must paginate the list.
	page1 := `{"errors":[],"messages":[],"data":[
		{"id":"other-1","type":"A","name":"a.example-lab.com","locked":0,"ttl":60,"value":"1.2.3.4",
		 "zone":{"id":"z-1","domain":{"id":"d-1","name":"example-lab.com"}}}
	],"status_code":200,"meta":{"pagination":{"total":2,"count":1,"per_page":1,"current_page":1,"total_pages":2}}}`
	page2 := `{"errors":[],"messages":[],"data":[
		{"id":"rec-1","type":"TXT","name":"tfacc-probe.example-lab.com","locked":1,"ttl":300,"value":"hello",
		 "zone":{"id":"z-1","domain":{"id":"d-1","name":"example-lab.com"}}}
	],"status_code":200,"meta":{"pagination":{"total":2,"count":1,"per_page":1,"current_page":2,"total_pages":2}}}`

	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Write([]byte(page1))
		case "2":
			w.Write([]byte(page2))
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	rec, err := c.GetRecord(context.Background(), "g-1", "z-1", "rec-1")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if rec.Name != "tfacc-probe.example-lab.com" || rec.Locked != 1 || rec.TTL != 300 {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.Zone.Domain.Name != "example-lab.com" {
		t.Errorf("zone.domain.name = %q", rec.Zone.Domain.Name)
	}
}

func TestGetRecordMissingIsErrNotFound(t *testing.T) {
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[],"messages":[],"data":[],"status_code":200,"meta":{"pagination":{"total":0,"count":0,"per_page":25,"current_page":1,"total_pages":1}}}`))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	_, err := c.GetRecord(context.Background(), "g-1", "z-1", "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUpdateAndDeleteRecord(t *testing.T) {
	var gotMethod, gotPath string
	var gotForm map[string][]string
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		r.ParseForm()
		gotForm = r.PostForm
		w.Write([]byte(`{"errors":[],"messages":[],"data":[],"status_code":200}`))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	if err := c.UpdateRecord(context.Background(), "g-1", "z-1", "rec-1", RecordInput{
		Name: "www.example-lab.com", Type: "A", TTL: 60, Value: "1.2.3.4",
	}); err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/groups/g-1/zones/z-1/records/rec-1" {
		t.Errorf("update went to %s %s", gotMethod, gotPath)
	}
	if gotForm["value"][0] != "1.2.3.4" {
		t.Errorf("update form = %v", gotForm)
	}

	if err := c.DeleteRecord(context.Background(), "g-1", "z-1", "rec-1"); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/groups/g-1/zones/z-1/records/rec-1" {
		t.Errorf("delete went to %s %s", gotMethod, gotPath)
	}
}

func TestCreateRecordValidationErrorCarriesFieldDetails(t *testing.T) {
	// Captured live: 400 validation errors carry a details map of per-field
	// messages ("The name must end with the current domain name.").
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status_code":400,"data":[],"messages":[],"errors":[{"code":"validation_failed","message":"Validation has failed, please fix","details":{"name":["The name must end with the current domain name."]}}]}`))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	_, err := c.CreateRecord(context.Background(), "g-1", "z-1", RecordInput{Name: "www", Type: "A", TTL: 60, Value: "1.2.3.4"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	if want := "The name must end with the current domain name."; !strings.Contains(err.Error(), want) {
		t.Errorf("error should carry field detail %q, got: %v", want, err)
	}
}
