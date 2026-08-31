// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"fmt"
)

// Envelope is the wrapper the Comlaude API puts around every response body:
// {errors, messages, data, status_code}. The wire shape of data is unreliable:
// single reads arrive as an object, collections as an array, and the OpenAPI
// spec's per-endpoint typing contradicts the wire (verified live), so data is
// kept raw until One or Many decides how to decode it.
type Envelope[T any] struct {
	Errors     []APIMessage    `json:"errors"`
	Messages   []APIMessage    `json:"messages"`
	Data       json.RawMessage `json:"data"`
	StatusCode int             `json:"status_code"`
}

// APIMessage is one entry of the envelope's errors/messages arrays.
type APIMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// One returns the single object carried by data, tolerating both wire shapes:
// a bare object, or an array whose first element is the object.
func (e *Envelope[T]) One() (T, error) {
	var zero T
	if len(e.Data) == 0 {
		return zero, fmt.Errorf("envelope has no data")
	}
	var obj T
	if err := json.Unmarshal(e.Data, &obj); err == nil {
		return obj, nil
	}
	var list []T
	if err := json.Unmarshal(e.Data, &list); err == nil {
		if len(list) == 0 {
			return zero, fmt.Errorf("envelope data is an empty array")
		}
		return list[0], nil
	}
	return zero, fmt.Errorf("envelope data is neither an object nor an array of objects")
}

// Many returns the collection carried by data, tolerating both wire shapes:
// an array, or a bare object treated as a one-element collection.
func (e *Envelope[T]) Many() ([]T, error) {
	if len(e.Data) == 0 {
		return nil, fmt.Errorf("envelope has no data")
	}
	var list []T
	if err := json.Unmarshal(e.Data, &list); err == nil {
		return list, nil
	}
	var obj T
	if err := json.Unmarshal(e.Data, &obj); err == nil {
		return []T{obj}, nil
	}
	return nil, fmt.Errorf("envelope data is neither an array nor an object")
}
