// Copyright (c) Tekaido Security
// SPDX-License-Identifier: MPL-2.0

package client

import "context"

// Profile is the authenticated user, the source of the fallback group id.
type Profile struct {
	ID      string   `json:"id"`
	GroupID string   `json:"group_id"`
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Roles   []string `json:"roles"`
}

// GetProfile returns the authenticated user's profile (GET /profile).
func (c *Client) GetProfile(ctx context.Context) (Profile, error) {
	return one[Profile](ctx, c, "GET", "/profile")
}
