# flate

Package `flate` implements the DEFLATE compressed data format specification (RFC 1951) with silicon-level optimizations, zero-allocation memory pooling, and SIMD-accelerated match copying.

## Benchmarks

Environment: `12th Gen Intel(R) Core(TM) i5-12400F`, `go version go1.25.4 windows/amd64`  
Benchmark command: `go test -bench=Benchmark.*Inflate -benchmem ./internal/compress/...`

| Engine | Execution Time | Heap Memory | Allocations | Comparison vs Klauspost |
|---|---|---|---|---|
| **`aoni/flate` (`Inflate`)** | **`2676 ns/op`** | **`0 B/op`** | **`0 allocs/op`** | **🔥 +269.2% faster (3.69x)**<br>**-100% memory (0 B)**<br>**0 allocs/op** |
| `klauspost/flate` | `9879 ns/op` | `7424 B/op` | `8 allocs/op` | baseline |
| `stdlib compress/flate` | `17722 ns/op` | `45552 B/op` | `15 allocs/op` | **-562.3% slower (6.62x)** |

## Optimizations

- **128-bit SIMD Wildcopy (`dictDecoder`)**: Match lengths $\le 8$ and $\le 16$ bytes are copied in hardware registers via `endian.Store64` and `endian.Load64`, completely bypassing `runtime.memmove` and `copy()` function call prologues.
- **32-byte Dual-Port SWAR Match Comparator (`matchLen`)**: 4-way unrolled 64-bit XOR pipeline utilizes parallel CPU execution ports (Port 2 & Port 3), eliminating branch mispredictions during long string matching.
- **RLE Micro-Engines**: Dedicated fast paths for 1-byte, 2-byte, and 4-byte repeating byte sequences (`dist == 1`, `dist == 2`, `dist == 4`) expand patterns using native machine word stores.
- **Static Precomputed Huffman Tables**: RFC 1951 fixed Huffman decoder tables are generated once during package `init()`, removing `sync.Once` atomic memory barriers from the critical path.
- **Per-P Storage Allocation Shielding**: Workers use `pool.PerPStorage` to bind readers to CPU cores (`GOMAXPROCS`), guaranteeing zero garbage collection overhead and zero runtime jitter under sustained high RPS.

## Usage

### Buffer Decompression

```go
decompressed, err := flate.Inflate(nil, compressedBytes)
if err != nil {
    return err
}
```

### Stream Decoding

```go
r := flate.NewReader(compressedStream)
defer r.Close()

io.Copy(dst, r)
```

### Monadic Result API

```go
res := compress.InflateResult(compressedBytes)
if res.IsSuccess() {
    data := res.MustValue()
    // process decompressed data
}
```
