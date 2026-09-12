# Borrow Architecture & Scoped Memory Semantics

This specification defines the zero-allocation memory borrowing model, scoped frame lifecycles, and buffer pool invariants implemented across `aoni` subsystems (`fast`, `codec`, `internal/fast`, `realtime`, `netutil`).

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

## 1. Core Principles of Borrowing

### The Scope Invariant
A borrowed buffer (`[]byte`) or zero-copy string view (`string`) is valid only for the duration of the current execution frame or callback scope.

1. **Immediate Consumption**: The consumer may read, parse, or inspect the borrowed memory within the active stack frame.
2. **No Implicit Retention**: Storing a borrowed slice or string reference into long-lived structs or passing it to asynchronous goroutines without copying is prohibited.
3. **Explicit Escaping**: Data retained past the frame lifetime must be explicitly cloned (`bytes.Clone`, `refkit.Clone`).

## 2. Subsystem Implementations

### Scoped WebSocket Frame Delivery (`realtime/ws`)
`aoni` implements `ReadMessageScoped` to avoid heap allocation per message:

```go
// ReadMessageScoped reads the next WebSocket message, allocating payload into scope.
// The payload slice MUST NOT be retained past the release of scope.
func (c *Conn) ReadMessageScoped(scope *borrow.Scope) (messageType int, payload []byte, err error)
```

1. **Buffer Borrowing**: The payload is allocated within the execution `*borrow.Scope`.
2. **Zero Allocation**: Eliminates GC tracking overhead via arena/slab recycling.
3. **Safe Return**: Memory is released in bulk when `scope.Release()` is called.

### HPACK & QPACK Header Borrowing
HTTP/2 (RFC 7541) and HTTP/3 (RFC 9204) decode header fields from binary frame streams using zero-copy byte slicing:
* **Static Table Lookups**: Return static, immutable header string references.
* **Literal Header Slices**: Sliced directly from the incoming frame buffer.
* **Zero-Copy Conversion**: Transient strings for header map lookups are cast using unsafe slice-to-string views.

### Zero-Allocation URL Values (`codec/values`)
* Traverses query bytes linearly via byte scanning.
* Emits key-value pairs by slicing the source buffer.
* Deserializes parameters into destination struct fields via compile-time reflection caches (`refkit`) without intermediate map allocations.

### gRPC-Web & Streaming Codecs
* **Framing Validation**: gRPC-Web framing (1-byte flag, 4-byte length) is validated in-place from the borrowed slab.
* **Proto Reflection**: Protobuf decoders allocate destination instances using `refkit.EnsureAlloc`, reusing underlying buffer pools.

## 3. Borrowing vs. Ownership Contract

| Operation | Memory Ownership | Lifetime | Allocation Cost | Retention Allowed? |
| :--- | :--- | :--- | :--- | :--- |
| `ReadMessageScoped(fn)` | Borrowed | Inside `fn` | 0 B | No (requires `bytes.Clone`) |
| `fast.RequestCtx.URI()` | Borrowed | Request Handler | 0 B | No (requires `.Copy()`) |
| `codec.Decode(buf, &v)` | Borrowed | Function Call | 0 B | Yes (if copied in struct) |
| `stream.Recv()` | Owned | Caller | $O(\text{struct})$ | Yes |

## 4. Safety & Concurrency Invariants

1. **Buffer Ownership Isolation**: A pooled buffer must be owned by at most one goroutine.
2. **Reset Before Release**: Buffers returned to `sync.Pool` must be truncated (`b = b[:0]`) and cleared of cryptographic/TLS content.
3. **Escape Detection**: Zero-allocation hot paths must be validated via allocation tests (`testing.AllocsPerRun`).
