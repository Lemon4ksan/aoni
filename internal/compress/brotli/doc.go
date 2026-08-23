// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package brotli provides an ultra-high-performance, RFC 7932 compliant Brotli
// decompression engine tailored for zero-allocation streaming and high-throughput
// network protocols.
//
// # Architecture & Silicon Characteristics
//
// The package implements a single-pass streaming decoder architecture with:
//   - 64-bit sliding bit-reader accumulator (bit_reader.go)
//   - Compact memory layout aligned for CPU L1/L2 cache locality (state.go)
//   - Zero-allocation static RFC 7932 dictionary slice (dictionary.go)
//   - Ring-buffer sliding window with zero heap churn across reuse cycles (ringbuffer.go)
//   - Direct in-memory fast-path for block decompression (Decompress)
//   - Fully pooled streaming readers implementing io.ReadCloser (AcquireReader, ReleaseReader)
package brotli
