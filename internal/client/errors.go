// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors the resource layer matches on with errors.Is to build
// diagnostics. The wrapped error text carries the API's own messages.
var (
	ErrAuth            = errors.New("authentication or authorization failed")
	ErrNotFound        = errors.New("not found")
	ErrPaymentRequired = errors.New("payment or entitlement required")
	ErrValidation      = errors.New("validation failed")
	ErrLocked          = errors.New("resource locked")
	ErrRateLimited     = errors.New("rate limited")
	ErrServer          = errors.New("server error")
)

// mapStatusError converts a non-2xx response into a typed error, pulling the
// API's own messages out of the envelope when the body carries one.
func mapStatusError(status int, body []byte) error {
	var env Envelope[struct{}]
	_ = json.Unmarshal(body, &env) // best effort; an empty envelope still maps
	msgs := env.Errors

	switch {
	case status == 400 || status == 422:
		return apiError(ErrValidation, status, msgs)
	case status == 401 || status == 403:
		return apiError(ErrAuth, status, msgs)
	case status == 402:
		return apiError(ErrPaymentRequired, status, msgs)
	case status == 404:
		return apiError(ErrNotFound, status, msgs)
	case status == 423:
		return apiError(ErrLocked, status, msgs)
	case status == 429:
		return apiError(ErrRateLimited, status, msgs)
	case status >= 500:
		return apiError(ErrServer, status, msgs)
	default:
		return apiError(fmt.Errorf("unexpected API response"), status, msgs)
	}
}

// apiError attaches the API's error messages to a sentinel.
func apiError(sentinel error, status int, msgs []APIMessage) error {
	if len(msgs) == 0 {
		return fmt.Errorf("%w (HTTP %d)", sentinel, status)
	}
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		parts = append(parts, m.Message)
		// Validation failures carry per-field details as an object of
		// message lists; other responses use an empty array. Render the
		// object form, ignore the rest.
		var details map[string][]string
		if len(m.Details) > 0 && json.Unmarshal(m.Details, &details) == nil {
			for field, fieldMsgs := range details {
				for _, fm := range fieldMsgs {
					parts = append(parts, fmt.Sprintf("%s: %s", field, fm))
				}
			}
		}
	}
	return fmt.Errorf("%w (HTTP %d): %s", sentinel, status, strings.Join(parts, "; "))
}
