# compress

Package `compress` provides a unified, zero-allocation multi-algorithm decompression subsystem for `aoni`, supporting RFC 1952 (gzip), RFC 1951 (deflate), RFC 7932 (brotli), and RFC 8878 (zstandard).

## Benchmarks

Environment: `12th Gen Intel(R) Core(TM) i5-12400F`, `go version go1.25.4 windows/amd64`  
Benchmark command: `go test -bench=Benchmark -benchmem ./internal/compress/...`

| Compression Format | Engine | Execution Time | Heap Memory | Allocations | Comparison vs Baseline |
|---|---|---|---|---|---|
| **RFC 1952 (Gzip)** | **`aoni`** | **`3683 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 +187.7% faster (2.88x vs Klauspost)**<br>**0 B/op (vs 7680 B)** |
| | `klauspost/gzip` | `10597 ns/op` | `7680 B/op` | `8 allocs/op` | baseline |
| | `stdlib compress/gzip` | `13109 ns/op` | `48912 B/op` | `14 allocs/op` | 3.56x slower than aoni |
| **RFC 1951 (Deflate)** | **`aoni`** | **`2676 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 +269.2% faster (3.69x vs Klauspost)**<br>**0 B/op (vs 7424 B)** |
| | `klauspost/flate` | `9879 ns/op` | `7424 B/op` | `8 allocs/op` | baseline |
| | `stdlib compress/flate` | `17722 ns/op` | `45552 B/op` | `15 allocs/op` | 6.62x slower than aoni |
| **RFC 8878 (Zstandard)** | **`aoni`** | **`129 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 Sub-microsecond wire speed** |
| **RFC 7932 (Brotli)** | **`aoni`** | **`66.3 µs` (100 KB)** | **`72 B/op`** | **`3 allocs/op`** | **🔥 1.32 GB/s wire throughput** |

## Architectural Pillars

- **Zero Allocations Across Hot Paths**: `Gunzip`, `Inflate`, and `Unzstd` execute with strict `0 B/op` and `0 allocs/op`.
- **Silicon Primitives (`foundation/silicon`)**: Codecs consume unified `cpukit` for CPU feature probes and `endian` for Little/Big Endian word loads and stores.
- **Core-Pinned Per-P Pooling**: Reusable decompressors are pooled per logical CPU core via `pool.PerPStorage`, eliminating GC mark-and-sweep latency spikes.
- **128-bit SIMD Wildcopy & SWAR**: Sub-16-byte dictionary lookups and prefix match evaluations execute in CPU registers without function call overhead.
- **Monadic Result API**: Functional error propagation via `generic.Result[T]` alongside standard Go idioms.

## Usage

### Direct Zero-Allocation Decompression

```go
data, err := compress.Gunzip(compressedGzip, nil)
data, err := compress.Inflate(compressedDeflate, nil)
data, err := compress.Unzstd(compressedZstd, nil)
data, err := compress.Unbrotli(compressedBrotli, nil)
```

### Universal Content-Encoding Dispatcher

```go
data, err := compress.Decompress("gzip", compressedBytes, nil)
```

### Monadic Result APIs

```go
res := compress.GunzipResult(compressedGzip)
if res.IsSuccess() {
    output := res.MustValue()
}
```
