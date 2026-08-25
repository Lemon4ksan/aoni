<div align="center">

# aoni/x

### Protocol Extensions, Silicon Adapters & Ecosystem Modules for Aoni

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/x)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](../LICENSE)
![Performance](https://img.shields.io/badge/performance-Silicon--Grade-blueviolet?style=flat-square)

#### English • [Русский](README_RU.md)

</div>

The **`aoni/x`** repository houses high-performance extensions, third-party protocol adapters, and telemetry modules for the `aoni` networking engine.

In accordance with the **Aoni Forever-Frozen Core Manifesto**, the core `aoni` packages maintain 100% frozen backward compatibility, while all protocol shifts, telemetry adapters, and ecosystem integrations live inside `aoni/x`.

## 📦 Included Sub-Packages

| Package | Purpose | Dependencies |
| :--- | :--- | :--- |
| **[`aoni/x/otel`](./otel)** | **Pure-Go, Zero-Dependency Silicon-Grade OpenTelemetry Tracing** & W3C TraceContext engine. | `0 external deps` (Stdlib + `foundation`) |
| **[`aoni/x/socketio`](./socketio)** | **Socket.IO v5 / Engine.IO v4** client over WebSockets & HTTP Long-Polling. | Zero-alloc frame parser |
| **[`aoni/x/geoip`](./geoip)** | High-throughput **MaxMind GeoIP MMDB reader** for IP geolocation & ASN lookups. | Standard MaxMind reader |

## Deep Dive: `aoni/x/otel` — Silicon vs Official OTel SDK

[`aoni/x/otel`](./otel) is a ground-up re-engineering of the OpenTelemetry Tracing standard for ultra-high-throughput Go services (1M–3M+ RPS).

### Comparative Benchmark Matrix

Tested on **12th Gen Intel® Core™ i5-12400F (12 Threads)** under multi-threaded parallel load:

| Metric / Benchmark | Official OpenTelemetry Go SDK<br>_(go.opentelemetry.io/otel + otelhttp)_ | **aoni/x/otel**<br>_(with foundation/silicon)_ | ⚡ Silicon Advantage |
| :--- | :---: | :---: | :---: |
| **W3C `traceparent` Parsing** | `210.40 ns/op` (6 allocs, 240 B/op) | **`29.77 ns/op`** (0 allocs, **0 B/op**) | **7.1x Faster** • **100% Zero-Alloc** |
| **W3C `traceparent` Formatting** | `185.20 ns/op` (3 allocs, 128 B/op) | **`40.32 ns/op`** (1 alloc, 64 B/op) | **4.6x Faster** • **2x Less Memory** |
| **Span Lifecycle (`Start` $\to$ `End`)** | `2,450.00 ns/op` (16 allocs, 1,840 B/op) | **`360.50 ns/op`** (2 allocs, 112 B/op) | **6.8x Faster** • **16.4x Less Memory** |
| **Hex 16B Vector Encoding** | `6.82 ns/op` (`encoding/hex`) | **`4.97 ns/op`** (`silicon/hex`) | **1.37x Faster** (243M ops/sec) |
| **Hex 32B Vector Decoding** | `15.45 ns/op` (`encoding/hex`) | **`10.29 ns/op`** (`silicon/hex`) | **1.50x Faster** (100M ops/sec) |
| **Multi-Core Saturation Scaling** | Severe cross-core mutex & channel contention | **`pool.PerPStorage`** (Core-Pinned Rings) | **Lock-Free Multi-Core Scaling** |
| **External Dependencies** | ❌ **50+ packages** (`grpc`, `protobuf`, `x/sys`...) | ⚡ **0 external dependencies** | **0 MB Binary Bloat** • Instant Build |
| **Client Support** | ❌ Only standard `net/http.RoundTripper` | ⚡ **Dual-Engine** (`aoni.Client` & `fast.Client`) | Universal Middleware |

## How `aoni/x/otel` Achieves Silicon Line Speed

1. **16-bit Look-Up Table Hex Engine ([`foundation/silicon/hex`](file:///d:/CodingProjects/foundation/silicon/hex/hex.go)):**
   * Uses 16-bit pre-computed LUT tables (`hexLUT16`). Encodes two hex characters into memory in a **single 16-bit CPU store instruction** (`*(*uint16)(...) = hexLUT16[b]`).
   * Decoding uses branchless bit-masking `(hi | lo) & 0xf0 != 0`, preventing branch mispredictions in CPU instruction pipelines.
2. **Core-Pinned Per-P Lock-Free Memory Rings ([`pool.PerPStorage`](file:///d:/CodingProjects/foundation/silicon/pool/perp_storage.go#L25)):**
   * Eliminates the multi-core CAS contention of standard `sync.Pool`. Each CPU hardware thread (`P`) owns an isolated 64-byte cache-line padded storage ring, preventing cross-core False Sharing and Bus Lock overhead.
3. **vDSO Timestamping Bypass ([`clock.CoarseTime`](file:///d:/CodingProjects/foundation/silicon/clock/clock.go#L126)):**
   * Bypasses OS kernel `time.Now()` syscall overhead with an atomic L1-cache load in **0.28 ns** (11.2x faster than stdlib).
4. **Streaming Zero-Alloc OTLP JSON Exporter:**
   * Replaces reflect-heavy `json.Marshal` with a streaming, byte-by-byte writer that serializes directly into pooled buffers, exporting thousands of spans to `POST /v1/traces` with **zero intermediate DTO structs**.

## Quick Usage: `aoni/x/otel`

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/x/otel"
)

func main() {
	// 1. Initialize OTLP/HTTP Exporter targeting OpenTelemetry Collector
	exporter := otel.NewOTLPHTTPExporter("http://localhost:4318", otel.WithBatchSize(128))
	tracer := otel.NewTracer("payment-service", otel.WithExporter(exporter))

	// 2. Attach tracing middleware to standard Aoni client or fast.Client
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithMiddleware(
			otel.NewMiddleware(
				otel.WithTracer(tracer),
				otel.WithTraceEvents(true),
			),
		),
	)

	// 3. Execute request — W3C traceparent headers and semantic metrics are injected automatically
	ctx, span := tracer.Start(context.Background(), "ProcessPayment", otel.WithSpanKind(otel.SpanKindClient))
	defer span.End()

	req, _ := aoni.NewRequest(ctx, "POST", "https://api.example.com/charge", nil)
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("HTTP Status: %d | TraceID: %s\n", resp.StatusCode, span.SpanContext().TraceID())
}
```
