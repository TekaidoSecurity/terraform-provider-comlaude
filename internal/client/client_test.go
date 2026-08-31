// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const loginOK = `{
	"errors": [],
	"messages": [{"code": "correct_login_details", "message": "Log-in details correct"}],
	"data": {"token_type": "Bearer", "expires_in": 7200, "access_token": "test-jwt", "refresh_token": "test-refresh"},
	"status_code": 200
}`

const loginBad = `{
	"errors": [{"code": "invalid_credentials", "message": "These credentials do not match our records"}],
	"messages": [],
	"data": [],
	"status_code": 401
}`

func TestLoginSendsFormCredentials(t *testing.T) {
	var gotForm map[string][]string
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api_login" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Write([]byte(loginOK))
	}))
	defer srv.Close()

	c := New(srv.URL, "user@example.com", "p4ss word", "key-123")
	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Errorf("content type = %q, want form-urlencoded", gotContentType)
	}
	for k, want := range map[string]string{
		"username": "user@example.com",
		"password": "p4ss word",
		"api_key":  "key-123",
	} {
		if got := gotForm[k]; len(got) != 1 || got[0] != want {
			t.Errorf("form[%s] = %v, want %q", k, got, want)
		}
	}
}

const profileOK = `{
	"errors": [],
	"messages": [],
	"data": {
		"id": "2cfe03e0-790e-45ea-8c1f-00006de03d9d",
		"group_id": "016592a1-793d-4218-bdd1-b2f25e89beae",
		"name": "svc@example.com",
		"roles": ["zone-manager", "domains-manager"]
	},
	"status_code": 200
}`

// newAuthedTestServer serves /api_login and requires the bearer token on
// every other path, delegating those to handler.
func newAuthedTestServer(t *testing.T, loginCount *int, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api_login" {
			if loginCount != nil {
				*loginCount++
			}
			w.Write([]byte(loginOK))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-jwt" {
			t.Errorf("Authorization = %q, want Bearer test-jwt", got)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(loginBad))
			return
		}
		handler(w, r)
	}))
}

func TestGetProfileAuthenticatesAndDecodes(t *testing.T) {
	logins := 0
	srv := newAuthedTestServer(t, &logins, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/profile" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(profileOK))
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	p, err := c.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if p.GroupID != "016592a1-793d-4218-bdd1-b2f25e89beae" {
		t.Errorf("GroupID = %q", p.GroupID)
	}
	if len(p.Roles) != 2 || p.Roles[0] != "zone-manager" {
		t.Errorf("Roles = %v", p.Roles)
	}
	if logins != 1 {
		t.Errorf("logins = %d, want 1 (lazy login before first call)", logins)
	}
}

func TestConcurrentCallsShareOneLogin(t *testing.T) {
	var mu sync.Mutex
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api_login" {
			mu.Lock()
			logins++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond) // widen the race window
			w.Write([]byte(loginOK))
			return
		}
		w.Write([]byte(profileOK))
	}))
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.GetProfile(context.Background()); err != nil {
				t.Errorf("GetProfile: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if logins != 1 {
		t.Errorf("logins = %d, want exactly 1 (single-flight)", logins)
	}
}

func TestExpiredTokenTriggersOneReloginAndReplay(t *testing.T) {
	logins := 0
	profileCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api_login" {
			logins++
			// Each login mints a distinct token so the replay provably
			// carries the fresh one.
			w.Write([]byte(strings.Replace(loginOK, "test-jwt", "jwt-"+string(rune('0'+logins)), 1)))
			return
		}
		profileCalls++
		if r.Header.Get("Authorization") != "Bearer jwt-2" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(loginBad))
			return
		}
		w.Write([]byte(profileOK))
	}))
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	p, err := c.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile after token expiry: %v", err)
	}
	if p.GroupID == "" {
		t.Error("expected decoded profile after replay")
	}
	if logins != 2 {
		t.Errorf("logins = %d, want 2 (initial + one re-login)", logins)
	}
	if profileCalls != 2 {
		t.Errorf("profile calls = %d, want 2 (original + one replay)", profileCalls)
	}
}

func TestPersistent401DoesNotLoop(t *testing.T) {
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api_login" {
			logins++
			w.Write([]byte(loginOK))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(loginBad))
	}))
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k")
	_, err := c.GetProfile(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("want ErrAuth, got %v", err)
	}
	if logins != 2 {
		t.Errorf("logins = %d, want 2 (initial + exactly one re-login, then give up)", logins)
	}
}

func TestStatusCodesMapToTypedErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    error
		wantMsg string
	}{
		{"404 not found", 404, `{"errors":[{"code":"not_found","message":"Resource not found"}],"messages":[],"data":[],"status_code":404}`, ErrNotFound, "Resource not found"},
		{"402 payment required", 402, `{"errors":[{"code":"payment_required","message":"Zone service not enabled"}],"messages":[],"data":[],"status_code":402}`, ErrPaymentRequired, "Zone service not enabled"},
		{"422 validation", 422, `{"errors":[{"code":"validation","message":"The ttl must be at least 1"}],"messages":[],"data":[],"status_code":422}`, ErrValidation, "The ttl must be at least 1"},
		{"423 locked", 423, `{"errors":[{"code":"locked","message":"Contact is in use"}],"messages":[],"data":[],"status_code":423}`, ErrLocked, "Contact is in use"},
		{"403 forbidden", 403, `{"errors":[{"code":"forbidden","message":"Missing role"}],"messages":[],"data":[],"status_code":403}`, ErrAuth, "Missing role"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			defer srv.Close()

			c := New(srv.URL, "u", "p", "k")
			_, err := c.GetProfile(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error should carry API message %q, got: %v", tc.wantMsg, err)
			}
		})
	}
}

func TestLoginBadCredentialsIsErrAuthWithAPIMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(loginBad))
	}))
	defer srv.Close()

	c := New(srv.URL, "user@example.com", "wrong", "key-123")
	err := c.Login(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuth) {
		t.Errorf("errors.Is(err, ErrAuth) = false; err = %v", err)
	}
	if !strings.Contains(err.Error(), "These credentials do not match our records") {
		t.Errorf("error should carry the API message, got: %v", err)
	}
}
