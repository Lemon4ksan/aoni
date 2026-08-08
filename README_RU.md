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

Разработка современных Go-приложений часто превращается в сборку «Франкенштейна» из десятка разрозненных сетевых библиотек: отдельных пакетов для HTTP/3, uTLS, DoH/DoQ, WebSockets, gRPC-Web, SSH-туннелей и ретраев. Каждая библиотека использует свои пулы памяти и контексты, вызывая проседания GC, раздувание кучи и непредсказуемые сбои соединений под нагрузкой.

`aoni` устраняет эту фрагментацию. Это **Единый Движок Сетевых Стандартов для Go**, объединяющий современные RFC IETF, спецификации W3C и механизмы надежности уровня Chromium в единой zero-alloc архитектуре.

Строите ли вы обычные REST-микросервисы, высоконагруженные API-шлюзы, реалтайм-сервисы на WebSockets или инструменты защиты и анализа - `aoni` обеспечивает предельные показатели производительности железа без компромиссов в функционале и надежности.

```shell
go get github.com/lemon4ksan/aoni
```

## Бескомпромиссная производительность: Доказано `pprof`

`aoni` работает на самом физическом пределе эффективности рантайма Go. Прямое сравнение с популярными HTTP-библиотеками на идентичных нагрузках показывает:

| Метрика | Стандартная обертка (Resty) | `aoni` (Стандартный) | `aoni` + `fast.Bridge` | `aoni/fast` (Нативный) | Преимущество `fast` |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **Задержка GET JSON (`ns/op`)** | 58 393 ns | 56 669 ns | 14 127 ns | **5 936 ns** | **В ~10 раз быстрее** |
| **Потребление памяти (`B/op`)** | 9 113 B | 8 217 B | 2 671 B | **363 B** | **В ~25 раз легче** |
| **Число аллокаций (`allocs/op`)** | 91 allocs | 82 allocs | 34 allocs | **8 allocs** | **В ~11 раз меньше аллокаций** |
| **Задержка HTTP/2 (`ns/op`)** | 76 519 ns | 75 958 ns | 71 200 ns | **68 164 ns** | **Быстрый H2 мультиплексинг** |
| **Задержка HTTP/3 (`ns/op`)** | 131 281 ns | 131 013 ns | 115 400 ns | **111 150 ns** | **Нативный H3 QUIC движок** |
| **Параллельная задержка (`ns/op`)** | 11 307 ns | 9 534 ns | 1 940 ns | **593 ns** | **В ~19 раз быстрее в параллель** |
| **Параллельная память и GC (`B / alloc`)** | 9 113 B / 91 | 8 217 B / 82 | 2 671 B / 34 | **0 B / 0 allocs** | **Абсолютный ноль аллокаций** |
| **Пиковая пропускная способность** | ~30k RPS | ~35k RPS | >70 000 RPS | **1 683 000+ RPS** | **Пиковая скорость кремния** |

Независимо от того, вызываете ли вы стандартные микросервисные REST API или парсите миллионы страниц за защитой Cloudflare/Akamai, `aoni` обеспечивает максимальную скорость без потерь.

## Единая эргономика

Независимо от того, выберете ли вы стандартный `aoni` или `aoni/fast`, вы будете управлять автомобилем с одинаково удобным рулевым колесом:

```
               ┌──► aoni.Client (100% совместимость с net/http и middleware)
option / mod ──┼
               └──► fast.Client (1.5M+ RPS multi-core, zero-alloc fasthttp + H2/H3)
```

* **Нужна 100% совместимость со стандартной библиотекой и сложное промежуточное ПО?** Используйте `aoni`.
* **Нужна абсолютная, чистая пропускная способность кремния и геометрия нулевого выделения памяти?** Используйте [`aoni/fast`](fast).

## Быстрый старт

### 1. Универсальный интерфейс `FetchTo`

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
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	// Запрос, валидация и декодирование за 1 вызов
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
// gRPC-Web вызов с 5-байтовым фреймингом и проверкой трейлеров
userResp, resp, err := fluent.PostGRPCWebTo[pb.UserResponse](ctx, client, "/UserService/GetUser", &pb.UserRequest{
	UserId: 42,
})
```

## Матрица возможностей

| Возможность / Функция | Go `net/http` | Стандартная обертка (напр., Resty) | `aoni` |
| :--- | :---: | :---: | :---: |
| **Пулирование билдера без аллокаций** | ✗ | ✗ | **✓ (`sync.Pool` Request Builder)** |
| **Декодирование через дженерики** | ✗ (Вручную) | ✗ (На интерфейсах) | **✓ (Безопасное `[T]`)** |
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

## Архитектура и подпакеты

```
aoni/
├── option/       // Опции инициализации клиента (option.With...)
├── mod/          // Модификаторы запросов (mod.With...)
├── request/      // Generic-хелперы (request.GetTo[T], PostTo, PostProtoTo)
├── fast/         // Безумно быстрый совместимый с net/http клиент на базе fasthttp
├── fluent/       // Цепочный Builder API (fluent.R, FetchTo[T], Codec)
├── cookie/       // Прокси-изолированные куки, формат Netscape, сортировка по RFC 6265
├── fingerprint/  // Обход TLS/JA4/p0f отпечатков, кадры HTTP/2, CDN-паддинг
├── netutil/      // Ротатор прокси, DoH/DoT DNS резолверы, ротатор IPv6 подсетей
├── codec/        // Декодеры ответов (JSON, Proto, gRPC-Web, XML) и кодирование параметров
├── realtime/     // WebSocket поверх H2 CONNECT, Socket.IO v5, SSE и NDJSON потоки
├── resiliency/   // Кэширование ответов, детекторы/солверы WAF, балансировщик нагрузки
└── telemetry/    // Генератор HAR, отслеживание задержек EWMA, веб-дашборд инспектора
```

## Документация и физика процессов

> **Нужны примеры использования?**  
> Загляните в директорию [examples](examples), чтобы узнать, как работать с aoni, а также в [примеры обхода блокировок](examples/evasions), чтобы увидеть интеграцию клиента с эмуляторами браузера вроде Playwright.

> **Интересует физика сетевых процессов?**  
> Прочитайте наше подробное руководство: [**Демистификация Вуду (Demystifying the Voodoo)**](docs/VOODOO.md), чтобы понять, как `aoni` манипулирует состояниями HPACK, переопределяет размеры окон TCP на уровне ОС с помощью системных вызовов и внедряет хаотический паддинг без разрыва соединений.

## Лицензия

Этот проект распространяется под лицензией **BSD 3-Clause**. Подробности смотрите в файле [LICENSE](LICENSE).

<div align="center">
  <sub>Сохраняйте холодный разум, оставайтесь непоколебимыми. Совсем как синий они.</sub>
</div>
