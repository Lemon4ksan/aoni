// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package offheap provides zero-GC arena memory allocation via direct OS kernel memory pages.
//
// # Architecture Overview
//
// Memory is allocated directly from the OS kernel, bypassing the Go runtime heap (mheap)
// and its garbage collector entirely. This eliminates GC mark/sweep overhead and latency
// jitter on high-throughput networking code paths.
//
// # Platform Backends
//
//   - Linux & macOS (Darwin): mmap(MAP_ANON|MAP_PRIVATE) via golang.org/x/sys/unix
//   - Windows: VirtualAlloc(MEM_COMMIT|MEM_RESERVE) via golang.org/x/sys/windows
//   - Other (FreeBSD, plan9, wasm): heap fallback - no off-heap guarantee
//
// # Thread Safety
//
//   - [Arena] is NOT thread-safe. One arena must not be shared across goroutines.
//   - [OffHeapBuffer] is NOT thread-safe.
//   - [ArenaPool] is thread-safe via sync.Pool. Use it for concurrent arena access.
//
// # GC Safety - CRITICAL
//
// Types allocated with [AllocStruct] MUST be Plain Old Data (POD) structures.
// They MUST NOT contain Go heap pointers, strings, maps, channels, or slice headers.
// The Go GC does NOT scan off-heap physical pages. Any Go heap object referenced from
// off-heap memory will appear unreachable to the GC and be collected, causing use-after-free.
//
// Safe POD types: structs containing only integer, float, bool, array, or fixed-size byte fields.
//
// Unsafe non-POD examples (FORBIDDEN inside AllocStruct):
//
//	type Bad struct {
//	    Name string      // ← heap pointer inside string header
//	    Data []byte      // ← heap pointer inside slice header
//	    Ch   chan int    // ← heap pointer
//	}
//
// # Volatile References
//
//   - [OffHeapBuffer.Bytes] returns a volatile slice. Invalid after [OffHeapBuffer.Release].
//   - [Arena.AllocBuffer] memory is arena-owned. Invalid after [Arena.Release] or [Arena.Reset].
//   - Pointers from [AllocStruct] are invalid after [Arena.Release] or [Arena.Reset].
//
// # Usage Patterns
//
// Request-scoped allocation with automatic cleanup:
//
//	err := offheap.Scope(1<<20, func(a *offheap.Arena) {
//	    hdr := offheap.AllocStruct[MyPODHeader](a)
//	    hdr.StreamID = 42
//	    // ... use hdr within this scope only
//	}) // arena freed on return, even on panic
//
// Concurrent reuse via pool:
//
//	pool := offheap.NewArenaPool(2 << 20)
//
//	func handler() {
//	    a := pool.Acquire()
//	    defer pool.Release(a)
//	    buf := a.AllocBuffer(4096)
//	    // ... use buf
//	}
//
// Standalone off-heap buffer:
//
//	buf, err := offheap.NewBuffer(64 * 1024)
//	if err != nil { ... }
//	defer buf.Release()
//
//	buf.WriteString("SAMPLE PAYLOAD\n")
//	// buf.Bytes() → zero-alloc slice view
package offheap
