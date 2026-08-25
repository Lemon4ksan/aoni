# compress

Package `compress` provides a unified, zero-allocation multi-algorithm compression and decompression subsystem for `aoni`, supporting RFC 1952 (gzip), RFC 1951 (deflate), RFC 7932 (brotli), and RFC 8878 (zstandard).

## Benchmarks

Environment: `12th Gen Intel(R) Core(TM) i5-12400F`, `go version go1.25.4 windows/amd64`  
Benchmark command: `go test -bench=Benchmark -benchmem ./internal/compress/...`

### Decompression (Wire to Memory)

| Compression Format | Engine | Execution Time | Heap Memory | Allocations | Comparison vs Baseline |
|---|---|---|---|---|---|
| **RFC 1952 (Gzip)** | **`aoni Gunzip`** | **`3107 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 4.08x faster vs stdlib**<br>**0 B/op (vs 48,912 B)** |
| | `stdlib compress/gzip` | `12661 ns/op` | `48912 B/op` | `14 allocs/op` | baseline |
| **RFC 1951 (Deflate)** | **`aoni Inflate`** | **`2588 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 5.37x faster vs stdlib**<br>**0 B/op (vs 45,552 B)** |
| | `stdlib compress/flate` | `13890 ns/op` | `45552 B/op` | `15 allocs/op` | baseline |
| **RFC 8878 (Zstandard)** | **`aoni Unzstd`** | **`249 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 Sub-microsecond wire speed** |
| **RFC 7932 (Brotli)** | **`aoni Unbrotli`** | **`3602 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 Zero allocations across all streams** |
| **Scoped Zero-Copy Gzip** | **`aoni GunzipScoped`** | **`2879 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | Bound to `borrow.Scope` arena |
| **Scoped Zero-Copy Zstd** | **`aoni UnzstdScoped`** | **`307 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | Bound to `borrow.Scope` arena |

### Compression (Memory to Wire)

| Compression Format | Engine | Execution Time | Heap Memory | Allocations |
|---|---|---|---|---|
| **RFC 1952 (Gzip)** | **`aoni Gzip`** | **`7430 ns/op`** | **`0 B/op`** | **`0 allocs/op`** |
| **RFC 1951 (Deflate)** | **`aoni Deflate`** | **`5671 ns/op`** | **`0 B/op`** | **`0 allocs/op`** |
| **RFC 7932 (Brotli)** | **`aoni Brotli`** | **`37.2 µs`** | **`72 B/op`** | **`2 allocs/op`** |

## Architectural Pillars

- **RFC 1952 `ISIZE` Trailer Prediction**: Gzip decompressor directly inspects the 4-byte `ISIZE` footer to pre-allocate exact destination capacity in a single shot, eliminating intermediate slice reallocations and copies.
- **Strict Zero Allocations Across Hot Paths**: `Gunzip`, `Inflate`, `Unzstd`, `Unbrotli`, `Gzip`, and `Deflate` execute with strict `0 B/op` and `0 allocs/op`.
- **Anti-Decompression-Bomb Shield**: Built-in protection automatically halts payload expansion exceeding `MaxAmplificationRatio` ($250\times$) or `DefaultMaxDecompressedSize` ($100\text{ MB}$) with `ErrDecompressionBomb` before memory exhaustion occurs.
- **Core-Pinned Per-P Pooling**: Reusable encoders and decoders are pooled per logical CPU core via `pool.PerPStorage`, eliminating lock contention and GC mark-and-sweep spikes.
- **Scoped Borrow Safety**: Full compatibility with `borrow.Scope` and static verification via the `vortex` borrow checker (Separation Logic, Structured Concurrency, Typestate Automata).

## Usage

### 1. Direct Zero-Allocation Decompression

```go
data, err := compress.Gunzip(compressedGzip, nil)
data, err := compress.Inflate(compressedDeflate, nil)
data, err := compress.Unzstd(compressedZstd, nil)
data, err := compress.Unbrotli(compressedBrotli, nil)
```

### 2. Universal Content-Encoding Dispatcher

```go
data, err := compress.Decompress("gzip", compressedBytes, nil)
```

### 3. Direct Zero-Allocation Compression

```go
compressed, err := compress.Gzip(rawBytes, nil)
compressed, err := compress.Deflate(rawBytes, nil)
compressed, err := compress.Brotli(rawBytes, nil)
compressed, err := compress.Compress("gzip", rawBytes, nil)
```

### 4. Zero-Allocation Scoped Decompression & Compression

```go
s := borrow.AcquireScope()
defer s.Release()

// Memory is allocated within the scope's slab and returned to pool on s.Release()
decompressed, err := compress.GunzipScoped(s, compressedGzip)
compressed, err := compress.GzipScoped(s, rawBytes)
```

### 5. Streaming Pooled Writers & Readers

```go
// Automatically flushes and returns compressors/decompressors to pool on Close()
w, err := compress.NewWriter("gzip", targetWriter)
_, err = w.Write(payload)
_ = w.Close()

r, err := compress.NewReader("gzip", sourceReader)
defer r.Close()
```

