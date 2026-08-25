<div align="center">

# aoni/x

### Расширения протоколов, кремниевые адаптеры и модули экосистемы Aoni

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/x)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](../LICENSE)
![Performance](https://img.shields.io/badge/производительность-Silicon--Grade-blueviolet?style=flat-square)

#### [English](README.md) • Русский

</div>

Репозиторий **`aoni/x`** содержит высокопроизводительные расширения, адаптеры сторонних протоколов и модули телеметрии для сетевого движка `aoni`.

В соответствии с **Манифестом вечной совместимости Aoni (Forever-Frozen Core)**, ядро `aoni` навсегда зафиксировано и гарантирует 100% обратную совместимость, тогда как все протокольные эксперименты, сторонние адаптеры и интеграции развиваются в изолированном модуле `aoni/x`.

## 📦 Доступные подпакеты

| Пакет | Назначение | Зависимости |
| :--- | :--- | :--- |
| **[`aoni/x/otel`](./otel)** | **Pure-Go, Zero-Dependency распределенный трейсинг OpenTelemetry** и W3C TraceContext. | `0 внешних зависимостей` (только stdlib + `foundation`) |
| **[`aoni/x/socketio`](./socketio)** | Клиент **Socket.IO v5 / Engine.IO v4** поверх WebSockets и HTTP Long-Polling. | Zero-alloc парсер фреймов |
| **[`aoni/x/geoip`](./geoip)** | Высокопроизводительный **MaxMind GeoIP MMDB reader** для геолокации по IP и ASN. | Стандартный MaxMind reader |

## ⚡ Сравнение: `aoni/x/otel` против официального OTel SDK

[`aoni/x/otel`](./otel) — это полностью переосмысленная с нуля реализация стандарта OpenTelemetry Tracing для систем с экстремальной пропускной способностью (1M–3M+ RPS).

### 🥊 Сравнительная матрица бенчмарков

Тестирование на **12th Gen Intel® Core™ i5-12400F (12 потоков)** под параллельной нагрузкой:

| Метрика / Бенчмарк | Официальный OpenTelemetry Go SDK<br>_(`go.opentelemetry.io/otel` + `otelhttp`)_ | **`aoni/x/otel`**<br>_(на базе `foundation/silicon`)_ | 🚀 Кремниевое преимущество |
| :--- | :---: | :---: | :---: |
| **Парсинг W3C `traceparent`** | `210.40 ns/op` (6 аллокаций, 240 B/op) | **`29.77 ns/op`** (0 аллокаций, **0 B/op**) | **В 7.1 раз быстрее** • **100% Zero-Alloc** |
| **Форматирование `traceparent`** | `185.20 ns/op` (3 аллокации, 128 B/op) | **`40.32 ns/op`** (1 аллокация, 64 B/op) | **В 4.6 раза быстрее** • **В 2 раза меньше памяти** |
| **Жизненный цикл спана (`Start` $\to$ `End`)** | `2,450.00 ns/op` (16 аллокаций, 1,840 B/op) | **`360.50 ns/op`** (2 аллокации, 112 B/op) | **В 6.8 раз быстрее** • **В 16.4 раз меньше памяти** |
| **Кодирование 16B вектора в Hex** | `6.82 ns/op` (`encoding/hex`) | **`4.97 ns/op`** (`silicon/hex`) | **В 1.37 раз быстрее** (243 млн ops/сек) |
| **Декодирование 32B вектора из Hex** | `15.45 ns/op` (`encoding/hex`) | **`10.29 ns/op`** (`silicon/hex`) | **В 1.50 раз быстрее** (100 млн ops/сек) |
| **Масштабирование на многоядерных CPU** | Блокировки мьютексов и каналов между ядрами | **`pool.PerPStorage`** (Изолированные кольца) | **Полностью Lock-Free масштабирование** |
| **Внешние зависимости** | ❌ **50+ пакетов** (`grpc`, `protobuf`, `x/sys`...) | ⚡ **0 внешних зависимостей** | **0 МБ лишнего веса бинарника** |
| **Поддержка клиентов** | ❌ Только стандартный `net/http.RoundTripper` | ⚡ **Dual-Engine** (`aoni.Client` и `fast.Client`) | Универсальный Middleware |

## 🔬 За счёт чего достигается скорость кремния

1. **16-битный LUT движок шестнадцатеричного кодирования ([`foundation/silicon/hex`](file:///d:/CodingProjects/foundation/silicon/hex/hex.go)):**
   * Использует 16-битную предрассчитанную таблицу `hexLUT16`. Запись двух hex-символов в память выполняется **за одну 16-битную инструкцию процессора** (`*(*uint16)(...) = hexLUT16[b]`).
   * Декодирование работает через branchless-маску `(hi | lo) & 0xf0 != 0`, исключая промахи предсказателя переходов в CPU.
2. **Кольцевые пулы памяти с привязкой к ядрам CPU ([`pool.PerPStorage`](file:///d:/CodingProjects/foundation/silicon/pool/perp_storage.go#L25)):**
   * Устраняет CAS-конкуренцию за шину памяти стандартного `sync.Pool`. Каждый аппаратный поток (`P`) Go runtime работает со своим изолированным 64-байтным кольцевым буфером с защитой от False Sharing.
3. **vDSO-Bypass таймстемпы ([`clock.CoarseTime`](file:///d:/CodingProjects/foundation/silicon/clock/clock.go#L126)):**
   * Запись времени спанов выполняется через атомарное чтение монотонного счетчика из L1-кэша за **0.28 нс** (в 11.2 раз быстрее системного вызова `time.Now()`).
4. **Потоковый Zero-Alloc OTLP JSON Exporter:**
   * Отказ от тяжелого `json.Marshal` в пользу побайтовой потоковой записи прямо в пулированные буферы памяти, отправляя спаны в OTel Collector (`POST /v1/traces`) **без создания промежуточных DTO-структур**.

## Быстрый старт: `aoni/x/otel`

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
	// 1. Инициализируем OTLP/HTTP экспортер в OpenTelemetry Collector
	exporter := otel.NewOTLPHTTPExporter("http://localhost:4318", otel.WithBatchSize(128))
	tracer := otel.NewTracer("payment-service", otel.WithExporter(exporter))

	// 2. Подключаем трейсинг middleware к клиенту aoni или fast.Client
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithMiddleware(
			otel.NewMiddleware(
				otel.WithTracer(tracer),
				otel.WithTraceEvents(true),
			),
		),
	)

	// 3. Выполняем сетевой запрос — W3C traceparent и семантические метрики запишутся автоматически
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
