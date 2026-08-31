// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// AmbiguousSupplierError reports that a zone could legally be created on
// more than one DNS supplier; the caller must choose.
type AmbiguousSupplierError struct {
	Candidates []Supplier
}

func (e *AmbiguousSupplierError) Error() string {
	return "more than one DNS supplier is available for this domain; set `supplier` to one of: " + supplierList(e.Candidates)
}

func supplierList(sups []Supplier) string {
	parts := make([]string, len(sups))
	for i, s := range sups {
		parts[i] = fmt.Sprintf("%q (key %s)", s.Name, s.Key)
	}
	return strings.Join(parts, ", ")
}

// ListSuppliers returns the account's suppliers (GET /suppliers), cached for
// the process lifetime: suppliers are stable account-wide reference data.
func (c *Client) ListSuppliers(ctx context.Context) ([]Supplier, error) {
	c.mu.Lock()
	cached := c.suppliers
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	sups, err := many[Supplier](ctx, c, "GET", "/suppliers?limit=1000")
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.suppliers = sups
	c.mu.Unlock()
	return sups, nil
}

// ResolveDNSSupplier picks the DNS supplier a new zone on the domain should
// use. A domain may hold at most one zone per supplier (live-verified), so
// suppliers already used on the domain are excluded. With an empty selector,
// the choice is automatic only when exactly one candidate remains; genuine
// ambiguity is surfaced as AmbiguousSupplierError, never guessed. A selector
// matches a supplier's id, key, or name.
func (c *Client) ResolveDNSSupplier(ctx context.Context, groupID, domainID, selector string) (Supplier, error) {
	sups, err := c.ListSuppliers(ctx)
	if err != nil {
		return Supplier{}, err
	}
	var dns []Supplier
	for _, s := range sups {
		if strings.HasPrefix(s.Key, "dns_supplier_") {
			dns = append(dns, s)
		}
	}
	sort.Slice(dns, func(i, j int) bool { return dns[i].Name < dns[j].Name })

	zones, err := c.ListZones(ctx, groupID, domainID)
	if err != nil {
		return Supplier{}, err
	}
	used := map[string]bool{}
	for _, z := range zones {
		if z.Supplier != nil {
			used[z.Supplier.ID] = true
		}
	}

	if selector != "" {
		for _, s := range dns {
			if s.ID == selector || s.Key == selector || strings.EqualFold(s.Name, selector) {
				if used[s.ID] {
					return Supplier{}, fmt.Errorf("supplier %q: the domain already has a zone on this supplier (one zone per supplier per domain)", s.Name)
				}
				return s, nil
			}
		}
		return Supplier{}, fmt.Errorf("no DNS supplier matches %q; available: %s", selector, supplierList(dns))
	}

	var candidates []Supplier
	for _, s := range dns {
		if !used[s.ID] {
			candidates = append(candidates, s)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return Supplier{}, fmt.Errorf("every DNS supplier (%s) already has a zone on this domain", supplierList(dns))
	default:
		return Supplier{}, &AmbiguousSupplierError{Candidates: candidates}
	}
}
