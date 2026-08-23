// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package extract provides ultra-high-speed, zero-allocation byte scraping and HTML tokenization primitives.
//
// It enables instant token, attribute, and payload boundary extraction directly from raw in-memory byte slices
// without parsing full DOM trees or allocating intermediate strings.
//
// # Core Primitives
//
//   - [Between]: Extracts raw sub-slices bounded between two delimiter strings with 0 heap allocations.
//   - [Attr]: Fast-tokenizes HTML elements matching simple CSS selectors and returns attribute byte views.
//   - [Regex]: Scrapes capture groups from byte slices via compiled regular expressions.
//   - [HTMLUnescape] / [AppendHTMLUnescape]: High-speed HTML entity unmarshaling directly into byte slices.
//
// # Swift-Inspired Integration with foundation/generic
//
// All extraction functions return standard Go `([]byte, error)` tuples that integrate seamlessly
// with [github.com/lemon4ksan/foundation/generic]:
//
//	// Standard (val, err) idiom:
//	csrfToken, err := extract.Between(body, `name="csrf_token" value="`, `"`)
//	if err != nil {
//	    return err
//	}
//
//	// Functional Result idiom:
//	res := generic.FromResult(extract.Between(body, `token="`, `"`))
//	if token, ok := res.Value(); ok {
//	    fmt.Printf("Token: %s\n", token)
//	}
package extract
