// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"testing"
)

// The Comlaude API wraps every response in {errors, messages, data, status_code}.
// On the wire, data arrives as an OBJECT for single reads and an ARRAY for
// collections, regardless of what the OpenAPI spec claims per endpoint
// (verified live; see docs/research/comlaude-api-summary.md).

type tokenPayload struct {
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	AccessToken string `json:"access_token"`
}

func TestEnvelopeOneUnwrapsArrayData(t *testing.T) {
	// The spec documents the login payload as data[0]; the wire sometimes
	// agrees. One() must unwrap the first element of an array transparently.
	raw := `{
		"errors": [],
		"messages": [],
		"data": [{"token_type": "Bearer", "expires_in": 7200, "access_token": "jwt-goes-here"}],
		"status_code": 200
	}`

	var env Envelope[tokenPayload]
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, err := env.One()
	if err != nil {
		t.Fatalf("One(): %v", err)
	}
	if got.AccessToken != "jwt-goes-here" {
		t.Errorf("unexpected payload: %+v", got)
	}
}

func TestEnvelopeOneErrorsOnEmptyArray(t *testing.T) {
	raw := `{"errors": [], "messages": [], "data": [], "status_code": 200}`
	var env Envelope[tokenPayload]
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := env.One(); err == nil {
		t.Fatal("expected an error for empty-array data, got none")
	}
}

func TestEnvelopeManyDecodesArrayData(t *testing.T) {
	// Shape captured live from GET .../records (data is an array).
	raw := `{
		"errors": [],
		"messages": [],
		"data": [
			{"token_type": "a"},
			{"token_type": "b"}
		],
		"status_code": 200
	}`
	var env Envelope[tokenPayload]
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := env.Many()
	if err != nil {
		t.Fatalf("Many(): %v", err)
	}
	if len(got) != 2 || got[0].TokenType != "a" || got[1].TokenType != "b" {
		t.Errorf("unexpected payload: %+v", got)
	}
}

func TestEnvelopeManyToleratesObjectData(t *testing.T) {
	raw := `{"errors": [], "messages": [], "data": {"token_type": "solo"}, "status_code": 200}`
	var env Envelope[tokenPayload]
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := env.Many()
	if err != nil {
		t.Fatalf("Many(): %v", err)
	}
	if len(got) != 1 || got[0].TokenType != "solo" {
		t.Errorf("unexpected payload: %+v", got)
	}
}

func TestEnvelopeDecodesObjectData(t *testing.T) {
	// Shape captured live from POST /api_login (data is an object).
	raw := `{
		"errors": [],
		"messages": [{"code": "correct_login_details", "message": "Log-in details correct"}],
		"data": {"token_type": "Bearer", "expires_in": 7200, "access_token": "jwt-goes-here"},
		"status_code": 200
	}`

	var env Envelope[tokenPayload]
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, err := env.One()
	if err != nil {
		t.Fatalf("One(): %v", err)
	}
	if got.TokenType != "Bearer" || got.ExpiresIn != 7200 || got.AccessToken != "jwt-goes-here" {
		t.Errorf("unexpected payload: %+v", got)
	}
}
