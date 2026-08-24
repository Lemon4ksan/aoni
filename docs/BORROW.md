# aoni Zero-Allocation Borrow Architecture & Scoped Memory Semantics

This specification defines the zero-allocation memory borrowing model, scoped frame lifecycles, and buffer pool safety invariants implemented across `aoni` hot paths (`fast`, `codec`, `internal/fast`, `realtime`, and `netutil`).

## 1. Architectural Motivation

In high-throughput Go network systems (exceeding 1.8M+ RPS), the primary throughput bottleneck is not CPU arithmetic or syscall execution, but Go Garbage Collector (GC) write barriers and heap allocation thrashing (`runtime.newobject`, `runtime.makeslice`, `runtime.growslice`).

`aoni` adopts a **borrowed memory model** inspired by systems-level linear types:
> _"Memory is allocated once in pooled ring buffers or frame slabs, borrowed by zero-copy reference across the parser stack, and reclaimed immediately upon frame completion."_

```text
               ┌─────────────────────────────────────────────────────────┐
               │           Pooled Slabs / OS Socket Read Ring            │
               └────────────────────────────┬────────────────────────────┘
                                            │
                               Borrow Slices (0 Allocs)
                                            │
               ┌────────────────────────────▼────────────────────────────┐
               │         Scoped Processing Frame / Fast Parser           │
               │   • HPACK / QPACK Header Views                          │
               │   • WebSocket Scoped Payloads                           │
               │   • URL Values & Query Deserialization                  │
               │   • gRPC-Web 5-Byte Prefix Framing                      │
               └────────────────────────────┬────────────────────────────┘
                                            │
                       ┌────────────────────┴────────────────────┐
                       │                                         │
               Scope Exit: Reclaim                       Caller Escape: Copy
                       │                                         │
               ┌───────▼───────────────┐                 ┌───────▼───────────────┐
               │ Returned to sync.Pool │                 │ Explicit bytes.Clone  │
               └───────────────────────┘                 └───────────────────────┘
```

## 2. Core Principles of Borrowing

### 2.1 The Scope Invariant
A borrowed buffer (`[]byte`) or zero-copy string view (`string`) is **valid only for the duration of the current execution frame or callback scope**.

1. **Immediate Consumption**: The consumer may read, parse, deserialize, or inspect the borrowed memory within the active stack frame without allocation.
2. **No Implicit Retention**: Storing a borrowed slice or string reference into long-lived structs, global maps, or passing it to asynchronous goroutines without copying is strictly prohibited.
3. **Explicit Escaping via Cloning**: When a caller must retain data past the frame lifetime, it MUST explicitly clone the memory (`bytes.Clone`, `refkit.Clone`, or string copy).

## 3. Subsystem Implementations

### 3.1 Scoped WebSocket Frame Delivery (`realtime/ws`)

Standard WebSocket implementations in Go allocate a fresh `[]byte` slice for every incoming message. Under parallel connections streaming tens of thousands of packets per second, this floods the GC heap.

`aoni` implements `ReadMessageScoped`:

```go
// ReadMessageScoped reads the next WebSocket message, passing a borrowed payload buffer.
// The payload slice MUST NOT be retained past the execution of fn.
func (c *Conn) ReadMessageScoped(fn func(messageType int, payload []byte) error) error
```

#### Mechanical Lifecycle:
1. **Buffer Borrowing**: The underlying connection loans its internal read slice directly to the callback `fn`.
2. **Zero Allocation**: No heap slice is allocated for the payload.
3. **Safe Return**: As soon as `fn` returns, the buffer offset is reset in the connection's read pool for the subsequent frame.

### 3.2 HPACK & QPACK Header Borrowing (`internal/fast/h2engine`, `internal/fast/h3engine`)

HTTP/2 (HPACK / RFC 7541) and HTTP/3 (QPACK / RFC 9204) decode header fields from binary frame streams.

`aoni` utilizes zero-copy byte slicing across header decompression routines:
- **Static Table Lookups**: Return static, immutable header string references directly from binary data structures without allocation.
- **Literal Header Slices**: Sliced directly from the incoming frame buffer.
- **Zero-Copy Conversion**: Transient strings needed for header map lookups are cast using unsafe slice-to-string views without heap copies.

### 3.3 Zero-Allocation URL Values & Query Mapper (`codec/values`)

Traditional `net/url.ParseQuery` creates a `map[string][]string`, allocating map nodes and slice headers for every query parameter.

The `aoni/codec/values` engine:
- Traverses query bytes linearly using native byte scanning.
- Emits key-value pairs by slicing the source buffer directly.
- Deserializes parameters into destination struct fields via compile-time reflection caches (`refkit`) without intermediate map allocations.

### 3.4 gRPC-Web & Streaming Codecs (`codec/decode`, `realtime/stream`)

1. **5-Byte Framing Validation**: gRPC and gRPC-Web prepend 1 byte of compression flag and 4 bytes of big-endian payload length. `aoni` validates framing in-place by reading from the borrowed slab.
2. **Proto Unmarshaling Reflection**: Generic protobuf decoders allocate destination message instances using `refkit.EnsureAlloc(val.Elem())`, avoiding pointer dereference panics while reusing underlying protobuf buffer pools.

## 4. Borrowing vs. Ownership Contract

| Operation | Memory Ownership | Lifetime | Allocation Cost | Retention Allowed? |
| :--- | :--- | :--- | :---: | :---: |
| `ReadMessageScoped(fn)` | Borrowed | Inside `fn` | **0 B** | ❌ No (requires `bytes.Clone`) |
| `fast.RequestCtx.URI()` | Borrowed | Request Handler | **0 B** | ❌ No (requires `.Copy()`) |
| `codec.Decode(buf, &v)` | Borrowed | Function Call | **0 B** | ✅ In struct if copied |
| `stream.Recv()` | Owned | Caller | $O(\text{struct})$ | ✅ Yes |

## 5. Safety & Concurrency Invariants

1. **Buffer Ownership Isolation**: A pooled buffer must be owned by at most one goroutine at any time.
2. **Reset Before Release**: All buffers returned to `sync.Pool` MUST be truncated (`b = b[:0]`) and cleared of sensitive byte contents if used in cryptographic or TLS contexts.
3. **Escape Detection in Tests**: All zero-allocation hot paths MUST be validated via memory allocation tests:
   ```go
   allocs := testing.AllocsPerRun(1000, func() {
       // Hot path execution
   })
   if allocs > 0 {
       t.Fatalf("expected 0 allocs, got %f", allocs)
   }
   ```
