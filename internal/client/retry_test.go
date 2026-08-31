// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetRetriesOn429And5xxThenSucceeds(t *testing.T) {
	attempts := 0
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		switch attempts {
		case 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.Write([]byte(profileOK))
		}
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k", WithRetryBaseDelay(time.Millisecond))
	if _, err := c.GetProfile(context.Background()); err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3 (429, 500, then success)", attempts)
	}
}

func TestGetGivesUpAfterMaxAttempts(t *testing.T) {
	attempts := 0
	srv := newAuthedTestServer(t, nil, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k", WithRetryBaseDelay(time.Millisecond))
	_, err := c.GetProfile(context.Background())
	if !errors.Is(err, ErrServer) {
		t.Fatalf("want ErrServer, got %v", err)
	}
	if attempts != 4 {
		t.Errorf("attempts = %d, want 4 (max attempts)", attempts)
	}
}

func TestLoginNeverRetriesOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":[{"code":"oops","message":"boom"}],"messages":[],"data":[],"status_code":500}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k", WithRetryBaseDelay(time.Millisecond))
	err := c.Login(context.Background())
	if !errors.Is(err, ErrServer) {
		t.Fatalf("want ErrServer, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want exactly 1 (POST must not retry on 5xx)", attempts)
	}
}

func TestLoginRetriesOn429(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(loginOK))
	}))
	defer srv.Close()

	c := New(srv.URL, "u", "p", "k", WithRetryBaseDelay(time.Millisecond))
	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2 (429 is safe to retry: the request was not processed)", attempts)
	}
}
