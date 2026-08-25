// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dict implements the IETF Compression Dictionary Transport specification
// (RFC 9842, RFC 9841, RFC 8878, RFC 9659).
//
// It provides automated HTTP dictionary discovery, parsing of "Use-As-Dictionary"
// response directives, RFC 8941 structured field negotiation via "Available-Dictionary"
// and "Dictionary-ID", zero-allocation URL pattern matching, and thread-safe LRU/TTL
// dictionary storage.
package dict
