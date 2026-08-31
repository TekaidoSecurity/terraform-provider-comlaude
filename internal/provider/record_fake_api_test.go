// Copyright IBM Corp. 2021, 2025
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

// fakeRecordAPI is an in-memory Comlaude API for record-resource tests. It
// mirrors behavior verified live during the tfacc-probe: create returns only
// {id}; create rejects names not ending in the zone's domain (400 with a
// details map); reads are list-only; records embed zone.domain.name.
type fakeRecordAPI struct {
	t *testing.T

	mu      sync.Mutex
	seq     int
	records map[string]map[string]fakeRecord // zoneID -> recordID -> record
	creates int

	// zoneDomain maps zoneID -> domain name it serves.
	zoneDomain map[string]string
}

type fakeRecord struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	TTL      int64   `json:"ttl"`
	Value    string  `json:"value"`
	Locked   int     `json:"locked"`
	Priority *int64  `json:"priority,omitempty"`
	Zone     any     `json:"zone"`
	priority *string // raw form value, for assertions
}

func newFakeRecordAPI(t *testing.T) (*fakeRecordAPI, *httptest.Server) {
	f := &fakeRecordAPI{
		t:          t,
		records:    map[string]map[string]fakeRecord{"z-1": {}, "z-2": {}},
		zoneDomain: map[string]string{"z-1": "example-lab.com", "z-2": "second-lab.com"},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeRecordAPI) zoneJSON(zoneID string) map[string]any {
	return map[string]any{
		"id": zoneID, "active": true,
		"domain": map[string]any{"id": "d-" + zoneID, "name": f.zoneDomain[zoneID]},
	}
}

func (f *fakeRecordAPI) envelope(data any) []byte {
	b, _ := json.Marshal(map[string]any{
		"errors": []any{}, "messages": []any{}, "data": data, "status_code": 200,
		"meta": map[string]any{"pagination": map[string]any{"total": 1, "count": 1, "per_page": 1000, "current_page": 1, "total_pages": 1}},
	})
	return b
}

func (f *fakeRecordAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := r.URL.Path
	switch {
	case path == "/api_login":
		w.Write([]byte(mockLoginOK))
	case path == "/profile":
		w.Write([]byte(mockProfileOK))

	case strings.HasSuffix(path, "/domains") && r.Method == http.MethodGet:
		// Domain list: used by the zone-domain resolver and data source.
		var domains []any
		for zid, dname := range f.zoneDomain {
			domains = append(domains, map[string]any{
				"id": "d-" + zid, "name": dname,
				"account":     map[string]any{"id": "acc-1", "name": "Test Account"},
				"nameservers": map[string]any{"names": []string{"ns1.example.com"}},
				"active_zone": map[string]any{"id": zid, "default_record_ttl": 86400, "resource_record_count": len(f.records[zid])},
			})
		}
		w.Write(f.envelope(domains))

	case strings.Contains(path, "/records"):
		f.handleRecords(w, r)

	default:
		f.t.Errorf("fake API: unexpected call %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeRecordAPI) handleRecords(w http.ResponseWriter, r *http.Request) {
	// Paths: /groups/{gid}/zones/{zid}/records[/{rid}]
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	zoneID := parts[3]
	store, ok := f.records[zoneID]
	if !ok {
		f.t.Errorf("fake API: unknown zone %q", zoneID)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	recordID := ""
	if len(parts) == 6 {
		recordID = parts[5]
	}

	switch {
	case r.Method == http.MethodGet:
		var list []fakeRecord
		for _, rec := range store {
			rec.Zone = f.zoneJSON(zoneID)
			list = append(list, rec)
		}
		w.Write(f.envelope(list))

	case r.Method == http.MethodPost:
		r.ParseForm()
		name := r.PostForm.Get("name")
		if !strings.HasSuffix(name, f.zoneDomain[zoneID]) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status_code":400,"data":[],"messages":[],"errors":[{"code":"validation_failed","message":"Validation has failed, please fix","details":{"name":["The name must end with the current domain name."]}}]}`))
			return
		}
		f.seq++
		f.creates++
		id := fmt.Sprintf("rec-%d", f.seq)
		store[id] = f.recordFromForm(id, r)
		w.Write([]byte(fmt.Sprintf(`{"errors":[],"messages":[],"data":{"id":%q},"status_code":200}`, id)))

	case r.Method == http.MethodPut:
		if _, exists := store[recordID]; !exists {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status_code":404,"data":[],"messages":[],"errors":[{"code":"not_found","message":"Record not found"}]}`))
			return
		}
		r.ParseForm()
		store[recordID] = f.recordFromForm(recordID, r)
		w.Write(f.envelope([]any{}))

	case r.Method == http.MethodDelete:
		delete(store, recordID)
		w.Write(f.envelope([]any{}))
	}
}

func (f *fakeRecordAPI) recordFromForm(id string, r *http.Request) fakeRecord {
	rec := fakeRecord{
		ID:   id,
		Name: r.PostForm.Get("name"),
		Type: r.PostForm.Get("type"),
	}
	fmt.Sscanf(r.PostForm.Get("ttl"), "%d", &rec.TTL)
	rec.Value = r.PostForm.Get("value")
	if p := r.PostForm.Get("priority"); p != "" {
		var v int64
		fmt.Sscanf(p, "%d", &v)
		rec.Priority = &v
	}
	return rec
}

// externalDelete simulates out-of-band deletion (drift).
func (f *fakeRecordAPI) externalDelete(zoneID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[zoneID] = map[string]fakeRecord{}
}

func (f *fakeRecordAPI) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates
}
