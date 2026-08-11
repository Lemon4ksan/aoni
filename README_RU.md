<div align="center">

# aoni

### Высокопроизводительный Zero-Alloc движок для Go HTTP, Protobuf и Real-Time сетей

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
![Resilience](https://img.shields.io/badge/stability-Chromium--Grade-blue?style=flat-square)

> _"В сетях хаос — это данность. Пусть aoni станет вашим ледяным якорем."_

#### [English](README.md) • Русский

</div>

## Почему Aoni?

Разработка современных Go-приложений часто требует объединения множества независимых сетевых библиотек - отдельного управления HTTP/3, uTLS, DoH/DoQ резолверами, WebSockets, gRPC-Web и ретраями. Использование разрозненных пулов памяти и моделей контекста приводит к повышенной нагрузке на сборщик мусора (GC), всплескам задержек и дублированию кода.

`aoni` объединяет современные стандарты RFC IETF, спецификации W3C и механизмы сетевой надежности уровня Chromium в единую архитектуру.

Независимо от того, выполняются ли стандартные запросы к REST API микросервисов, высоконагруженная маршрутизация на API-шлюзе, реалтайм-потоки WebSockets или задачи сетевого анализа, `aoni` обеспечивает пути выполнения с нулевыми аллокациями и предсказуемым бюджетом памяти.

```shell
go get github.com/lemon4ksan/aoni
```

## Быстрый старт

### 1. Универсальный Generic-интерфейс (`FetchTo`)

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(15*time.Second),
		option.WithChrome(), // Готовый профиль Chrome, ECH, 0-RTT и устойчивость к сбоям
	)

	// Выполнение запроса, валидация и декодирование в 1 вызов
	user, resp, err := fluent.FetchTo[User](ctx, client, "GET", "/users/{id}",
		mod.WithVar("id", 123),
		mod.WithHeader("X-Custom-Header", "value"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("User: %s (Status: %d)\n", user.Name, resp.StatusCode)
}
```

### 2. Прямая поддержка Protobuf и gRPC-Web

```go
// Вызов gRPC-Web с 5-байтовым фреймингом и валидацией трейлеров
userResp, resp, err := fluent.PostGRPCWebTo[pb.UserResponse](ctx, client, "/UserService/GetUser", &pb.UserRequest{
	UserId: 42,
})
```

## Архитектура и Двойной Движок

`aoni` предоставляет два исполнительных движка под единой моделью API:

```
               ┌──► aoni.Client (100% совместимость с net/http и цепями middleware)
option / mod ──┼
               └──► fast.Client (1.5M+ RPS multi-core, zero-alloc fasthttp + H2/H3)
```

* **Стандартный `aoni.Client`**: Используется, когда требуется 100% совместимость со стандартной библиотекой Go и инфраструктурой `net/http`.
* **Нативный `fast.Client`**: Используется, когда необходимы максимальная пропускная способность и геометрия памяти с 0 B/op аллокаций.

## Производительность и Профиль `pprof`

Следующие бенчмарки `pprof` замеряют задержку выполнения, потребление памяти в куче и количество аллокаций при идентичной нагрузке:

| Метрика | Resty (`net/http`) | `aoni` (Стандартный) | `aoni` + `fast.Bridge` | `aoni/fast` (Нативный) | Разница производительности |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **Задержка GET JSON (`ns/op`)** | 58 393 ns | 56 669 ns | 14 127 ns | **5 703 ns** | **В ~5 раз быстрее (Bridge) / в 10 раз (Native)** |
| **Память в куче (`B/op`)** | 9 113 B | 8 217 B | 2 671 B | **363 B** | **В ~3.4 раза легче (Bridge) / в 25 раз (Native)** |
| **Аллокации (`allocs/op`)** | 91 allocs | 82 allocs | 34 allocs | **8 allocs** | **В ~2.7 раза меньше (Bridge) / в 11 раз (Native)** |
| **Задержка HTTP/2 (`ns/op`)** | 76 519 ns | 75 958 ns | 71 200 ns | **68 164 ns** | **Ускоренный H2 мультиплексинг** |
| **Задержка HTTP/3 (`ns/op`)** | 131 281 ns | 131 013 ns | 115 400 ns | **111 150 ns** | **Нативный H3 QUIC движок** |
| **Параллельная задержка (`ns/op`)** | 11 307 ns | 9 534 ns | 1 940 ns | **589.9 ns** | **В ~6 раз быстрее (Bridge) / в 19 раз (Native)** |
| **Параллельная память и GC (`B / alloc`)** | 9 113 B / 91 | 8 217 B / 82 | 2 671 B / 34 | **0 B / 0 allocs** | **Ноль аллокаций в куче** |
| **Пиковая пропускная способность** | ~30k RPS | ~35k RPS | >70 000 RPS | **1 695 000+ RPS** | **Высоконагруженный I/O** |

> [!TIP]
> Высокая нагрузка в стандартных HTTP-клиентах Go вызывает частые паузы сборщика мусора (GC) и задержки `mark-assist`, выбивающие p99-показатели задержки.
> За счет повторного использования буферов через `sync.Pool` и нативного SIMD AVX2 ускорения (`simd_amd64.s`), `aoni/fast` работает с **0 B/op и 0 allocs/op** в параллельном I/O. Полностью изолируя рантайм Go от давления GC, `aoni` соперничает и превосходит HTTP-стеки языков без сборки мусора (таких как Rust `reqwest` / `hyper`), обеспечивая плоскую наносекундную задержку и 1.695M+ RPS на одном сервере.

> [!NOTE]
> `fast.Bridge` оборачивает `aoni.Client` для сохранения совместимости с `net/http.Client`, снижая задержку с ~58мкс до 14.1мкс. Нативный `aoni/fast` обеспечивает **1.695M+ RPS** под параллельной нагрузкой со 100% отсутствием аллокаций (**0 B/op**) в горячем цикле.

## Поддерживаемые Протоколы и Возможности

| Возможность / Функция | Go `net/http` | Стандартная обертка (напр., Resty) | `aoni` |
| :--- | :---: | :---: | :---: |
| **Пулирование билдера без аллокаций** | ✗ | ✗ | **✓ (`sync.Pool` Request Builder)** |
| **Декодирование через дженерики** | ✗ (Вручную) | ✗ (На интерфейсах) | **✓ (Типобезопасное `[T]`)** |
| **Нативный Protobuf и gRPC-Web** | ✗ | ✗ | **✓ (Binary, Text и Stream)** |
| **Параллельное подключение «Happy Eyeballs»** | ⚠️ (Базовое) | ✗ | **✓ (RFC 8305)** |
| **Активный Circuit Breaking** | ✗ | ✗ | **✓ (Встроенный Middleware)** |
| **Вежливый парсинг `Retry-After`** | ✗ | ✗ | **✓ (Delta-sec и RFC1123)** |
| **Автоматическое декодирование не-UTF8** | ✗ | ✗ | **✓ (Автоматически)** |
| **Обход TLS-анализа (JA3/JA4)** | ✗ | ✗ | **✓ (Через `uTLS` и Handshake)** |
| **Снятие отпечатков JA4+** | ✗ | ✗ | **✓ (TLS и HTTP, на чистом Go)** |
| **Поддержка Unix Domain Sockets** | ⚠️ (Вручную) | ✗ | **✓ (Нативная схема `unix://`)** |
| **Клиент Socket.IO / Engine.IO v4** | ✗ | ✗ | **✓ (Полная спецификация v5)** |
| **Изоляция сессий и кук по прокси** | ✗ | ✗ | **✓ (`ProxyIsolatedJar`)** |
| **Переопределения на уровне запроса** | ✗ (Ручной транспорт) | ✗ (Требует клонирования клиента) | **✓ (Контекстные аксессоры)** |

## Структура Репозитория

```
aoni/
├── option/       // Опции инициализации клиента (option.With...)
├── mod/          // Модификаторы запросов (mod.With...)
├── request/      // Generic-хелперы (request.GetTo[T], PostTo, PostProtoTo)
├── fast/         // Движок высокой производительности на базе fasthttp
├── fluent/       // Цепочный Builder API (fluent.R, FetchTo[T], Codec)
├── cookie/       // Прокси-изолированные куки, формат Netscape, сортировка по RFC 6265
├── fingerprint/  // Обход TLS/JA4/p0f отпечатков, кадры HTTP/2, CDN-паддинг
├── netutil/      // Ротатор прокси, DoH/DoT DNS резолверы, ротатор IPv6 подсетей
├── codec/        // Декодеры ответов (JSON, Proto, gRPC-Web, XML) и кодирование параметров
├── realtime/     // WebSocket поверх H2 CONNECT, Socket.IO v5, SSE и NDJSON потоки
├── resiliency/   // Кэширование ответов, детекторы/солверы WAF, балансировщик нагрузки
└── telemetry/    // Генератор HAR, отслеживание задержек EWMA, веб-дашборд инспектора
```

## Примеры использования в реальном мире

- **[discordgo-aoni](https://github.com/lemon4ksan/discordgo-aoni)**: Высокопроизводительный форк официального `discordgo` с нулевыми аллокациями на базе движков `aoni` и `aoni/realtime/ws`.
  - Обеспечивает **рост пропускной способности REST API в 6.8 раз (203 000+ RPS)** и **ускорение WebSocket в 3.1 раза** при **0 B/op** аллокациях памяти на фреймах.

## Техническая Спецификация и Документация

- [**Network Stack Specification**](docs/NETWORK_STACK.md): Детальный обзор механик Happy Eyeballs v3, авто-восстановления HTTP 421/408/425, ECH и работы с пулами.
- [**CPU & Silicon Sympathy Specification**](docs/CPU_STACK.md): Архитектура нативного PLAN9 AVX2 SIMD ассемблера (`simd_amd64.s`), арены памяти 2MB LargePages и наносекундный бюджет инструкций CPU.
- [**Demystifying the Voodoo**](docs/VOODOO.md): Разбор манипуляций состояниями HPACK, тюнинга окон TCP через сисколлы и джиттера сетевых пакетов.
- [**Примеры кода**](examples): Исполняемые примеры использования REST, WebSockets, gRPC-Web и обхода блокировок.

## Лицензия

Распространяется под лицензией **BSD 3-Clause License**. См. [LICENSE](LICENSE) для деталей.

<div align="center">
  <sub>Сохраняйте холодный разум, оставайтесь непоколебимыми. Совсем как синий они.</sub>
</div>
