// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package optimizer provides compiler optimization passes over the vortex Intermediate Representation (IR).
//
// Prior to code emission, the optimizer analyzes the IR to achieve zero runtime allocations,
// optimal CPU cache-line alignment, deterministic HTTP serialization, and multi-endpoint connection pooling.
//
// # Optimization Passes:
//
//  1. Sub-Requester Clustering: Partitions service methods by distinct base URLs into dedicated,
//     pre-configured Requester instances, eliminating per-request URL re-parsing and connection pool churn.
//  2. Stack Allocation Sizing: Pre-calculates exact stack array dimensions for request modifier slices
//     and query/form byte buffers, aligned to powers of two and CPU L1 cache-line boundaries (64 bytes).
//  3. Query Canonicalization: Deterministically orders query parameters to enable zero-alloc loop encoding
//     and maximize HTTP/2 HPACK header compression efficiency (RFC 7541).
//  4. Header Normalization: Deduplicates static headers and canonicalizes HTTP header keys (RFC 9110).
package optimizer
