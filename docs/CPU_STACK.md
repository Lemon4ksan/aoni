# Silicon Sympathy & CPU Performance Stack

This specification documents the low-level hardware alignment, native PLAN9 x86-64 SIMD assembly routines, and kernel-bypass I/O subsystems.

## 1. Execution Rules
1. **Zero Heap Allocation**: Hot execution paths (`fast`, `codec`, `simd`, `internal/pipeline`) must not trigger heap allocations.
2. **64-Byte Cache Line Alignment**: Memory buffers and atomic structs are sized to 64-byte L1 data cache lines to prevent false sharing.
3. **No Dynamic Reflection**: Interface dispatch is avoided in I/O loops; type resolution uses compile-time generics.
4. **AVX-to-SSE Transition Guard**: Assembly executing 256-bit AVX2 vectors must issue `VZEROUPPER` before returning to standard runtime frames.

## 2. Native PLAN9 x86-64 Assembly (`internal/simd`)

```text
              +-------------------------------------------------------+
              |              32-Byte Payload Vector                   |
              +-------------------------------------------------------+
                                          |
                                VPCMPEQB  v  VPBROADCASTB
              +-------------------------------------------------------+
              |             256-bit Vector Comparison                 |
              +-------------------------------------------------------+
                                          |
                                VPMOVMSKB v  BSFL / VZEROUPPER
              +-------------------------------------------------------+
              |            32-bit Bitmask & Bit-Scan Index            |
              +-------------------------------------------------------+
```

* **`indexByteAVX2`**: Scans buffers for a byte using 256-bit AVX2 vectors (`VPBROADCASTB`, `VPCMPEQB`, `VPMOVMSKB`, `BSFL`).
* **`indexTwoBytesAVX2`**: Scans HTTP headers for dual delimiters (`:` or `\n`).
* **`applyFastMaskAVX2`**: Executes RFC 6455 WebSocket payload masking (`VPXOR`).

## 3. Kernel I/O Acceleration (`internal/sysnet`)

* **Windows RIO**: Registers user-space memory buffers via `RIORegisterBuffer`, eliminating page pinning overhead in `WSASend`/`WSARecv`.
* **Linux Zero-Copy Sockets**: Applies socket options (`SO_MARK`, `TCP_MAXSEG`, `TCP_QUICKACK`) directly.

## 4. Instruction Budget Limits & Protocol Benchmarks

*Hardware: Intel Core i5-12400F @ 4.4 GHz, 12 threads.*

| Metric Tier | Throughput | Microarchitectural Limit / Bound |
| :--- | :--- | :--- |
| Request Pool Lifecycle | 122,891,942 Ops/sec | ~42 CPU Clock Cycles |
| URL Template & Cache | 22,548,748 Ops/sec | Zero-Alloc Byte Slice Pool |
| Fast Engine Core (In-Memory Pipeline) | 2,126,754 RPS | ~2480 Clock Cycles / Tx |
| AVX2 VPXOR Masker | 69.02 GB/sec | Memory Bus Bandwidth |
| WebSocket 1KB Masking | 68.69 GB/sec | L1/L2 Write Bandwidth |
| OS Socket Network (HTTP/1.1 TCP Parallel) | 151,446 – 193,000 RPS | OS Kernel Socket Bottleneck (6.6 µs latency) |
| OS Socket Network (HTTP/2 TLS Multiplex) | 60,569 – 69,300 RPS | TLS Framing + HPACK Table (16.5 µs latency) |
| OS Socket Network (HTTP/3 QUIC Parallel) | 1,091 RPS | Bidirectional QUIC Stream FSM (0.91 ms latency) |

## 5. Gollvm (LLVM 20+) Integration

Compiling `aoni` with `llvm-goc` integrates LLVM middle-end scalar evolution and vectorization passes:
* **Huffman Vectorization**: Shifts HPACK encoding from 324 MB/s to 697.8 MB/s (2.15x speedup) via 64-bit barrel shifter registers.
* **ASCII Folding**: Reduces HTTP header lookup from 8.47 ns to 1.71 ns (4.95x speedup) via branchless vectors.
* **Floating-Point Filter**: EWMA latency filters leverage FMA instructions (1.43x speedup).
