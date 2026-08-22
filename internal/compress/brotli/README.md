# brotli

Package `brotli` provides a high-performance Pure-Go implementation of the Brotli decompression algorithm (RFC 7932).

## Benchmarks

Environment: `12th Gen Intel(R) Core(TM) i5-12400F`, `go version go1.25.4 windows/amd64`  
Benchmark command: `go test -bench=BenchmarkCompare -benchmem ./internal/compress/brotli/...`

| Payload Class | `fasthttp` (`andybalholm/brotli`) | `aoni/brotli` | Difference |
|---|---|---|---|
| **Small Payload (1 KB JSON)** | `1992 ns/op` (44.18 MB/s)<br>`120 B/op` (5 allocs) | `1814 ns/op` (48.50 MB/s)<br>`72 B/op` (3 allocs) | **+9.8% faster**<br>**-40.0% memory**<br>**-2 allocs/op** |
| **Medium Payload (18 KB HTML)** | `22356 ns/op` (800.69 MB/s)<br>`122 B/op` (5 allocs) | `16003 ns/op` (1118.53 MB/s)<br>`72 B/op` (3 allocs) | **+39.7% faster**<br>**-41.0% memory**<br>**-2 allocs/op** |
| **Large Payload (100 KB Data)** | `101254 ns/op` (862.18 MB/s)<br>`140 B/op` (5 allocs) | `66351 ns/op` (1315.74 MB/s)<br>`72 B/op` (3 allocs) | **+52.6% faster**<br>**-48.6% memory**<br>**1.32 GB/s wire speed** |
| **Stream Reuse (`DecompressReuse`)** | `22314 ns/op` (802.18 MB/s) | `15305 ns/op` (1169.54 MB/s) | **+45.8% faster**<br>**1.17 GB/s** |

## Optimizations

- **Pipelined Register Decoding**: Unrolled symbol lookups in `command.go` execute in CPU registers without intermediate memory roundtrips.
- **Per-P Worker Pooling**: Utilizes `pool.PerPStorage[*Reader]` to pin decoder instances to logical cores (`GOMAXPROCS`), avoiding GC collection cycles.
- **64-bit SWAR Context Evaluation**: `detectTrivialLiteralBlockTypes` verifies 64-byte blocks in 8 branchless 64-bit word operations.
- **Vectorized Memory Copies**: Constant 16-byte slice staging lowers to hardware 128-bit `MOVOU` instructions. Fast RLE loop for single-byte distance runs (`dist == 1`).
- **Bounds Check Elimination**: Direct indexed lookups replace multi-stage slice headers in Huffman decoding routines.
- **Decompression Bomb Protection**: Output budget limiting via `Reader.SetMaxOutputSize(maxBytes)`.

## Usage

### Stream Decoding

```go
r := brotli.NewReader(compressedStream)
defer r.Close()

io.Copy(dst, r)
```

### Buffer Decompression

```go
decompressed, err := brotli.Decompress(nil, compressedBytes)
if err != nil {
    return err
}
```

### Monadic Result API

```go
res := brotli.DecodeResult(compressedBytes)
if data, ok := res.Value(); ok {
    // process decompressed data
}
```
