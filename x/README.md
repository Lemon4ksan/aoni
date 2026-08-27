<div align="center">

# aoni/x

### Protocol Extensions & Auxiliary Modules for Aoni

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/x)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](../LICENSE)

#### English • [Русский](README_RU.md)

</div>

The **`aoni/x`** package provides extensions, third-party protocol adapters, and telemetry modules for the `aoni` networking engine.

The core `aoni` package maintains a stable API surface, while experimental protocols and third-party integrations live inside `aoni/x`.

## 📦 Included Sub-Packages

| Package | Purpose | Dependencies |
| :--- | :--- | :--- |
| **[`aoni/x/otel`](./otel)** | OpenTelemetry Distributed Tracing & W3C TraceContext implementation. | 0 external dependencies (stdlib only) |
| **[`aoni/x/socketio`](./socketio)** | **Socket.IO v5 / Engine.IO v4** client over WebSockets & HTTP Long-Polling. | Low-allocation frame parser |
| **[`aoni/x/geoip`](./geoip)** | **MaxMind GeoIP MMDB reader** for IP geolocation & ASN lookups. | MaxMind format compatible |

## ⚡ Comparison: `aoni/x/otel` vs Official OTel SDK

[`aoni/x/otel`](./otel) is an optimized implementation of the OpenTelemetry Tracing standard for high-throughput services.

### Comparative Benchmarks

Tested on **12th Gen Intel® Core™ i5-12400F (12 Threads)** under multi-threaded parallel load:

| Metric / Operation | Official OpenTelemetry Go SDK<br>_(go.opentelemetry.io/otel + otelhttp)_ | **aoni/x/otel** | Delta |
| :--- | :---: | :---: | :---: |
| **W3C `traceparent` Parsing** | `210.40 ns/op` (6 allocs, 240 B/op) | **`29.77 ns/op`** (0 allocs, **0 B/op**) | **7.1x Faster** (0 B/op) |
| **W3C `traceparent` Formatting** | `185.20 ns/op` (3 allocs, 128 B/op) | **`40.32 ns/op`** (1 alloc, 64 B/op) | **4.6x Faster** |
| **Span Lifecycle (`Start` $\to$ `End`)** | `2,450.00 ns/op` (16 allocs, 1,840 B/op) | **`360.50 ns/op`** (2 allocs, 112 B/op) | **6.8x Faster** |
| **Hex 16B Vector Encoding** | `6.82 ns/op` (`encoding/hex`) | **`4.97 ns/op`** (`silicon/hex`) | **1.37x Faster** |
| **Hex 32B Vector Decoding** | `15.45 ns/op` (`encoding/hex`) | **`10.29 ns/op`** (`silicon/hex`) | **1.50x Faster** |
| **Multi-Core Scaling** | Mutex lock contention | **`pool.PerPStorage`** (Isolated buffers) | Reduced contention |
| **External Dependencies** | ❌ 50+ packages | ⚡ **0 external dependencies** | Minimal binary footprint |
| **Client Support** | ❌ Only `net/http.RoundTripper` | ⚡ `aoni.Client` & `fast.Client` | Universal Middleware |

## Implementation Details

1. **Table-Driven Hex Encoding:** Uses 16-bit pre-computed lookup tables (`hexLUT16`) to speed up trace ID serialization.
2. **Per-P Storage Pools (`pool.PerPStorage`):** Core-local buffers reduce cross-core lock contention.
3. **Monotonic Clocks:** Direct coarse time access for span timestamping.
4. **Streaming OTLP JSON Exporter:** Serializes trace spans directly into wire buffers without intermediate DTO allocations.

## Quick Usage: `aoni/x/otel`

```go
package main

import (
	"context"
	"fmt"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/x/otel"
)

func main() {
	// 1. Initialize OTLP/HTTP Exporter
	exporter := otel.NewOTLPHTTPExporter("http://localhost:4318", otel.WithBatchSize(128))
	tracer := otel.NewTracer("payment-service", otel.WithExporter(exporter))

	// 2. Attach tracing middleware to client
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithMiddleware(
			otel.NewMiddleware(
				otel.WithTracer(tracer),
				otel.WithTraceEvents(true),
			),
		),
	)

	// 3. Execute request
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
