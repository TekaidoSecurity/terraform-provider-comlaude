// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeZoneAPI is an in-memory zones API for the comlaude_zone resource
// tests. Zones live on domain d-1; deleting an active zone is refused the
// way the docs describe (the resource should never even attempt it).
type fakeZoneAPI struct {
	t *testing.T

	mu        sync.Mutex
	suppliers []map[string]any
	seq       int
	zones     map[string]map[string]any // zoneID -> zone object
	creates   int
	payment   bool // when true, create returns 402
	requests  []string
}

func newFakeZoneAPI(t *testing.T) (*fakeZoneAPI, *httptest.Server) {
	f := &fakeZoneAPI{t: t, suppliers: []map[string]any{
		{"id": "s-1", "name": "NS1", "key": "dns_supplier_ns1"},
		{"id": "s-2", "name": "Com Laude DNS", "key": "dns_supplier_comlaude"},
		{"id": "s-3", "name": "Registrar X", "key": "registrar_x"},
	}, zones: map[string]map[string]any{
		// Mirrors the live test domain: one pre-existing active zone whose
		// supplier is the default for new zones.
		"z-live": {"id": "z-live", "active": true, "signed": false, "networks": []int{0, 9002},
			"default_record_ttl": int64(86400), "supplier": map[string]any{"id": "s-1", "name": "NS1", "key": "dns_supplier_ns1"}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeZoneAPI) envelope(data any) []byte {
	b, _ := json.Marshal(map[string]any{
		"errors": []any{}, "messages": []any{}, "data": data, "status_code": 200,
	})
	return b
}

func (f *fakeZoneAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	path := r.URL.Path
	switch {
	case path == "/api_login":
		w.Write([]byte(mockLoginOK))
	case path == "/profile":
		w.Write([]byte(mockProfileOK))

	case path == "/suppliers":
		var list []any
		for _, s := range f.suppliers {
			list = append(list, s)
		}
		w.Write(f.envelope(list))

	case strings.Contains(path, "/domains/d-1/zones"):
		f.handleZones(w, r)

	default:
		f.t.Errorf("fake zone API: unexpected call %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeZoneAPI) handleZones(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	zoneID := ""
	if len(parts) == 6 {
		zoneID = parts[5]
	}

	switch {
	case r.Method == http.MethodPost:
		if f.payment {
			w.WriteHeader(http.StatusPaymentRequired)
			w.Write([]byte(`{"status_code":402,"data":[],"messages":[],"errors":[{"code":"payment_required","message":"Zone service is not enabled for this account"}]}`))
			return
		}
		r.ParseForm()
		if r.PostForm.Get("supplier_id") == "" {
			// Live-verified: supplier_id is required despite the spec.
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status_code":400,"data":[],"messages":[],"errors":[{"code":"validation_failed","message":"Validation has failed, please fix","details":{"supplier_id":["The supplier id field is required."]}}]}`))
			return
		}
		f.seq++
		f.creates++
		id := fmt.Sprintf("zone-%d", f.seq)
		z := map[string]any{"id": id, "active": r.PostForm.Get("active") == "1", "signed": false, "networks": []int{0, 9002}, "default_record_ttl": 86400,
			"supplier": map[string]any{"id": r.PostForm.Get("supplier_id"), "name": "NS1", "key": "dns_supplier_ns1"}}
		if ttl := r.PostForm.Get("default_record_ttl"); ttl != "" {
			var v int64
			fmt.Sscanf(ttl, "%d", &v)
			z["default_record_ttl"] = v
		}
		f.zones[id] = z
		w.Write(f.envelope(z))

	case r.Method == http.MethodGet && zoneID == "":
		var list []any
		for _, z := range f.zones {
			list = append(list, z)
		}
		w.Write(f.envelope(list))

	case r.Method == http.MethodGet && zoneID != "":
		z, ok := f.zones[zoneID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status_code":404,"data":[],"messages":[],"errors":[{"code":"not_found","message":"Zone not found"}]}`))
			return
		}
		w.Write(f.envelope(z))

	case r.Method == http.MethodPatch:
		z, ok := f.zones[zoneID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		r.ParseForm()
		if ttl := r.PostForm.Get("default_record_ttl"); ttl != "" {
			var v int64
			fmt.Sscanf(ttl, "%d", &v)
			z["default_record_ttl"] = v
		}
		if a := r.PostForm.Get("active"); a != "" {
			z["active"] = a == "1"
		}
		w.Write(f.envelope(z))

	case r.Method == http.MethodDelete:
		if z, ok := f.zones[zoneID]; ok && z["active"] == true {
			// The API refuses to delete active zones; the resource must
			// fail with its own diagnostic BEFORE this is ever reached.
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status_code":400,"data":[],"messages":[],"errors":[{"code":"zone_active","message":"Active zones cannot be deleted"}]}`))
			return
		}
		delete(f.zones, zoneID)
		w.Write(f.envelope([]any{}))
	}
}

func (f *fakeZoneAPI) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates
}

func (f *fakeZoneAPI) externalDeleteZones() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id := range f.zones {
		if id != "z-live" {
			delete(f.zones, id)
		}
	}
}

func (f *fakeZoneAPI) addSupplier(id, name, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suppliers = append(f.suppliers, map[string]any{"id": id, "name": name, "key": key})
}

func (f *fakeZoneAPI) setPaymentRequired(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payment = v
}
