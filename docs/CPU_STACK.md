# aoni Silicon Sympathy & CPU Performance Stack Specification

This specification documents the low-level hardware alignment, zero-allocation memory architectures, native PLAN9 x86-64 SIMD assembly routines, and kernel-bypass I/O subsystems of the `aoni` network engine.

## 1. Architectural Principles & Mechanical Sympathy

The `aoni` silicon stack is engineered around modern x86-64 microarchitectural realities (Intel Golden Cove / AMD Zen 4+ execution pipelines, L1/L2/L3 cache hierarchies, and memory bus bandwidth limitations).

### 1.1 Core Execution Rules
1. **Zero Heap Allocation Target**: Hot execution paths (`fast`, `codec`, `simd`, `internal/pipeline`) MUST NOT trigger Go heap allocations (`runtime.newobject` / `runtime.makeslice`).
2. **64-Byte Cache Line Alignment**: Memory buffers and atomic state structures are sized and padded to align with 64-byte L1 data cache lines to eliminate false sharing across CPU threads.
3. **No Dynamic Reflection**: Interface dispatch and type assertions are avoided in inner I/O loops; type resolution is handled via compile-time generics or direct struct alignment.
4. **AVX-to-SSE State Transition Guard**: Every SIMD vector assembly entry point executing 256-bit AVX2 vector instructions MUST issue `VZEROUPPER` before returning control to standard Go runtime frames to prevent CPU legacy SSE transition penalties.

## 2. Native PLAN9 x86-64 Assembly Trinity (`internal/simd`)

High-throughput payload scanning and frame masking are delegated to native Go PLAN9 assembly routines (`simd_amd64.s`), operating on 32-byte vector blocks per instruction cycle.

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

### 2.1 32-Byte Vector Byte Scanner (`indexByteAVX2`)

Scans arbitrary byte buffers for a target character byte using 256-bit AVX2 vectors.

```text
Input Parameters:
  - b_base   : unsafe.Pointer (R8)
  - b_len    : int            (CX)
  - c        : byte           (DX)
Output:
  - index    : int            (AX) [-1 if not found]
```

#### Execution Pipeline:
1. **Broadcast**: `VPBROADCASTB` replicates the target byte across all 32 bytes of vector register `YMM0`.
2. **Vector Loop**: Iterates through 32-byte chunks loaded via unaligned vector move `VMOVDQU (R8), YMM1`.
3. **Vector Comparison**: `VPCMPEQB YMM0, YMM1, YMM2` compares bytes in parallel, producing `0xFF` for matches and `0x00` for non-matches.
4. **Bitmask Extraction**: `VPMOVMSKB YMM2, DX` extracts the 32-bit byte match mask into general-purpose register `DX`.
5. **Bit Scan**: `BSFL DX, AX` locates the exact trailing zero bit index corresponding to the first matching byte offset.
6. **Cleanup**: Executes `VZEROUPPER` to restore upper AVX YMM register states.

### 2.2 Dual-Byte Header Separator Scanner (`indexTwoBytesAVX2`)

Scans HTTP header payload streams simultaneously for target delimiters (e.g., `:` or `\n`).

#### Execution Pipeline:
1. **Dual Broadcast**: Broadcasts byte `c1` into `YMM0` and byte `c2` into `YMM1`.
2. **Parallel Comparison**: Executes `VPCMPEQB` against both target vectors.
3. **Bitwise Bitmask Composition**: Merges match masks via `VPOR YMM2, YMM3, YMM4` before issuing `VPMOVMSKB`.
4. **Index Calculation**: Computes the first index matching either byte using `BSFL`.

### 2.3 256-Bit Payload XOR Masker (`applyFastMaskAVX2`)

Executes RFC 6455 §5.3 WebSocket client payload masking at hardware bus speed.

$$\text{Throughput} = \frac{32\text{ bytes}}{\text{1 instruction cycle}} \approx 69.0\text{ GB/sec}$$

#### Execution Pipeline:
1. **Mask Broadcast**: `VPBROADCASTD` broadcasts the 4-byte XOR mask across all 8 dwords of `YMM0`.
2. **Vector Unrolling**: Loads 32-byte payload slices (`VMOVDQU`), performs vector XOR (`VPXOR YMM0, YMM1, YMM1`), and writes back output (`VMOVDQU YMM1, (R8)`).
3. **Tail Byte Processing**: Any residual bytes (< 32 bytes) are processed using single-byte XOR logic.

## 3. Kernel I/O Acceleration Subsystems (`internal/sysnet`)

For operating systems supporting direct kernel memory registration, `aoni` provides optional registered I/O acceleration hooks.

### 3.1 Windows Registered I/O (RIO via `mswsock.dll`)
- **Mechanism**: Registers user-space memory buffers with Winsock kernel drivers via `RIORegisterBuffer`.
- **Elimination of Page Pinning**: Standard `WSASend` / `WSARecv` syscalls lock user-space memory pages in physical RAM on every socket call. RIO pre-locks memory pages once upon client initialization, reducing Win32 kernel transition overhead.

### 3.2 Linux Zero-Copy Sockets & Socket Options
- **Mechanism**: Socket controllers apply socket options (`SO_MARK`, `TCP_MAXSEG`, `TCP_QUICKACK`) directly prior to TCP SYN dispatch.

## 5. Instruction Budget & Microarchitectural Limits

Empirical micro-benchmarking using `cmd/aoni-bench` establishes the following physical performance boundaries on 12-thread x86-64 hardware (Intel Core i5-12400F @ 4.4 GHz):

```text
+------------------------------------+-----------------------+----------------------------------+
| Metric Tier                        | Benchmark Throughput  | Microarchitectural Limit         |
+------------------------------------+-----------------------+----------------------------------+
| 1. Request Pool Lifecycle          | 122,891,942 Ops/sec   | ~42 CPU Clock Cycles (L1 Cache)  |
| 2. URL Template & LRU Cache        |  22,548,748 Ops/sec   | Zero-Alloc Byte Slice Pool       |
| 3. Fast Engine Core (Pipelined)    |   1,689,131 RPS       | ~2500 Clock Cycles / Transaction |
| 4. AVX2 VPXOR Memory Masker        |      69.02 GB/sec     | Dual-Channel Memory Bus Limit    |
| 5. TLS Stealth Profile Synthesis   |   8,239,675,686 Ops/sec| Pure Register Pointer Assignment |
| 6. WebSocket 1KB Frame Masking     |      68.69 GB/sec     | Vector L1/L2 Write Bandwidth     |
| 7. OS Socket Network (127.0.0.1)   |     154,508 RPS       | OS Kernel Network Stack Bottleneck|
+------------------------------------+-----------------------+----------------------------------+
```

### 5.1 Nanosecond Budget Calculations

1. **Pool Checkout (`122.8M Ops/sec`)**:
   $$\text{Nanoseconds per operation} = \frac{1,000,000,000\text{ ns}}{122,891,942} \approx 8.13\text{ ns}$$
   At a 4.4 GHz CPU clock frequency, $8.13\text{ ns} \times 4.4\text{ GHz} \approx 35.7\text{ cycles/core}$. This matches the physical latency bound of L1 data cache access (4-5 cycles) + 1 atomic CAS instruction.

2. **AVX2 Vector Throughput (`69.02 GB/sec`)**:
   $$69.02\text{ GB/sec} \approx 69,020,000,000\text{ bytes/sec}$$
   Reading and XORing 32 bytes per AVX2 vector instruction at 4.4 GHz consumes the maximum hardware bandwidth of the system's memory controller and L3 cache bus interface.

## 6. Gollvm (LLVM 20+) Compiler Acceleration

`aoni` is fully compatible with **`Gollvm`** (`llvm-goc`), unlocking LLVM's advanced middle-end scalar evolution, vectorization passes, loop unrolling, and architecture-tuned code generation:

### 6.1 Build Profiles & Flags

```bash
# Environment setup (WSL / Linux)
export PATH="/home/senya/gollvm-install/bin:$PATH"
export LD_LIBRARY_PATH="/home/senya/gollvm-install/lib64:$LD_LIBRARY_PATH"

# Dynamic build with CPU-specific vector instruction selection
go build -gccgoflags="-O3 -march=native" -o aoni_app main.go

# Fully static binary (eliminates libgo.so runtime shared object dependency)
go build -gccgoflags="-O3 -static-libgo" -o aoni_static main.go

# Direct LLVM intermediate assembly emission
llvm-goc -fgo-pkgpath=main -O3 -S -o output.s main.go
```

### 6.2 Microarchitectural LLVM Optimizations in Aoni
- **Huffman Stream Vectorization**: LLVM synthesizes multi-bit shift-or sequences into 64-bit barrel shifter registers, boosting HPACK encoding throughput from 324 MB/s to **697.8 MB/s (2.15x speedup)**.
- **Branchless ASCII Folding**: Case-insensitive HTTP header matching compiles into branchless vector operations, reducing lookup latency from 8.47 ns to **1.71 ns (4.95x speedup)**.
- **Pipelined Floating-Point Filter**: EWMA latency filters leverage fused multiply-accumulate (FMA) for **1.43x speedup**.

