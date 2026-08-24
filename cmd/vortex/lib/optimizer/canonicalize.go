// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer

import (
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// canonicalizeQueryParams preserves original parameter order to match the Go interface contract.
func canonicalizeQueryParams(svc *ir.ServiceIR) {
	// No-op: parameter order in Go methods must strictly match the Go interface declaration.
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
