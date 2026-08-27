<div align="center">

# aoni/x

### Расширения протоколов и дополнительные модули для Aoni

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/x)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](../LICENSE)

#### [English](README.md) • Русский

</div>

Модуль **`aoni/x`** содержит расширения, адаптеры протоколов и инструменты телеметрии для сетевого стека `aoni`.

Базовое ядро `aoni` сохраняет стабильный API, в то время как специфичные протокольные адаптеры и интеграции со сторонними экосистемами развиваются в пакете `aoni/x`.

## 📦 Доступные подпакеты

| Пакет | Назначение | Особенности |
| :--- | :--- | :--- |
| **[`aoni/x/otel`](./otel)** | Распределенный трейсинг OpenTelemetry и W3C TraceContext. | Без внешних зависимостей (только stdlib) |
| **[`aoni/x/socketio`](./socketio)** | Клиент **Socket.IO v5 / Engine.IO v4** поверх WebSockets и HTTP Long-Polling. | Парсер фреймов с минимальными аллокациями |
| **[`aoni/x/geoip`](./geoip)** | Чтение баз **MaxMind GeoIP MMDB** для геолокации по IP и ASN. | Совместимость с MaxMind форматом |
| **[`aoni/x/webtransport`](./webtransport)** | **WebTransport поверх HTTP/3** (W3C / RFC 9297) для датаграмм и стримов. | Мультиплексирование QUIC и HTTP/3 |
| **[`aoni/x/grpc/dynamic`](./grpc/dynamic)** | **Динамический gRPC-клиент** через дескрипторы Protobuf и JSON. | Динамические схемы без кодогенерации |
| **[`aoni/x/sqlcookie`](./sqlcookie)** | **SQL-хранилище cookies** для изолированных персистентных jar. | Драйвер `database/sql` |
| **[`aoni/x/tunnel/tun`](./tunnel/tun)** | **Низкоуровневые TUN/TAP драйверы L3** (Wintun, Linux tun, macOS utun). | Нативные платформенные драйверы |

## ⚡ Сравнение: `aoni/x/otel` и OpenTelemetry SDK

[`aoni/x/otel`](./otel) — оптимизированная реализация OpenTelemetry Tracing для высоконагруженных систем.

### Сравнительные результаты бенчмарков

Тестирование на **12th Gen Intel® Core™ i5-12400F (12 потоков)** под параллельной нагрузкой:

| Метрика / Операция | OpenTelemetry Go SDK<br>_(`go.opentelemetry.io/otel` + `otelhttp`)_ | **`aoni/x/otel`** | Разница |
| :--- | :---: | :---: | :---: |
| **Парсинг W3C `traceparent`** | `210.40 ns/op` (6 аллокаций, 240 B/op) | **`29.77 ns/op`** (0 аллокаций, **0 B/op**) | **В 7.1 раз быстрее** (0 B/op) |
| **Форматирование `traceparent`** | `185.20 ns/op` (3 аллокации, 128 B/op) | **`40.32 ns/op`** (1 аллокация, 64 B/op) | **В 4.6 раза быстрее** |
| **Жизненный цикл спана (`Start` $\to$ `End`)** | `2,450.00 ns/op` (16 аллокаций, 1,840 B/op) | **`360.50 ns/op`** (2 аллокации, 112 B/op) | **В 6.8 раз быстрее** |
| **Кодирование 16B вектора в Hex** | `6.82 ns/op` (`encoding/hex`) | **`4.97 ns/op`** (`silicon/hex`) | **В 1.37 раз быстрее** |
| **Декодирование 32B вектора из Hex** | `15.45 ns/op` (`encoding/hex`) | **`10.29 ns/op`** (`silicon/hex`) | **В 1.50 раз быстрее** |
| **Масштабирование на многоядерных CPU** | Блокировки мьютексов | **`pool.PerPStorage`** (Изолированные буферы) | Сниженная конкуренция |
| **Внешние зависимости** | ❌ 50+ пакетов | ⚡ **0 внешних зависимостей** | Минимальный размер бинарника |
| **Поддержка клиентов** | ❌ Только `net/http.RoundTripper` | ⚡ `aoni.Client` и `fast.Client` | Универсальный Middleware |

## Оптимизации реализации

1. **Табличное Hex-кодирование:** Использование 16-битных предрассчитанных таблиц (`hexLUT16`) для ускорения сериализации идентификаторов трассировки.
2. **Пулы памяти Per-P (`pool.PerPStorage`):** Локальные для потоков выполнения буферы снижают конкуренцию между ядрами CPU.
3. **Монотонные метки времени:** Быстрое получение времени для фиксации спанов.
4. **Потоковый OTLP JSON Exporter:** Сериализация данных напрямую в буфер отправки без промежуточных аллокаций структур.

## Быстрый старт: `aoni/x/otel`

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
	// 1. Инициализация OTLP/HTTP экспортера
	exporter := otel.NewOTLPHTTPExporter("http://localhost:4318", otel.WithBatchSize(128))
	tracer := otel.NewTracer("payment-service", otel.WithExporter(exporter))

	// 2. Подключение middleware трейсинга к клиенту
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithMiddleware(
			otel.NewMiddleware(
				otel.WithTracer(tracer),
				otel.WithTraceEvents(true),
			),
		),
	)

	// 3. Выполнение запроса
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
