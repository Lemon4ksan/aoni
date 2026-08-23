// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer

import (
	"cmp"
	"slices"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// canonicalizeQueryParams deterministically sorts query parameters by WireKey.
func canonicalizeQueryParams(svc *ir.ServiceIR) {
	if svc == nil {
		return
	}

	for _, m := range svc.Methods {
		if m == nil {
			continue
		}

		// Stable sort parameters by location first, then by WireKey for query parameters
		slices.SortStableFunc(m.Params, func(a, b *ir.ParamIR) int {
			if a == nil || b == nil {
				return 0
			}

			// Keep context as the very first parameter always
			if a.Location == ir.LocContext {
				return -1
			}

			if b.Location == ir.LocContext {
				return 1
			}

			// Sort query params alphabetically by wire key
			if a.Location == ir.LocQuery && b.Location == ir.LocQuery {
				return cmp.Compare(a.WireKey, b.WireKey)
			}

			return 0
		})
	}
}

// deduplicateHeaders removes redundant duplicate headers on the same method.
func deduplicateHeaders(svc *ir.ServiceIR) {
	if svc == nil {
		return
	}

	for _, m := range svc.Methods {
		if m == nil || len(m.Headers) <= 1 {
			continue
		}

		seen := make(map[string]int) // HeaderKey (lowercase) -> index

		var uniqueHeaders []ir.HeaderIR

		for _, h := range m.Headers {
			key := strings.ToLower(h.Key)
			if idx, exists := seen[key]; exists {
				// Override previous duplicate header value while preserving original key casing
				uniqueHeaders[idx].StaticValue = h.StaticValue
				uniqueHeaders[idx].DynamicTemplate = h.DynamicTemplate
				continue
			}

			seen[key] = len(uniqueHeaders)
			uniqueHeaders = append(uniqueHeaders, h)
		}

		m.Headers = uniqueHeaders
	}
}
