<div align="center">

# aoni

### Унифицированный стек сетевых протоколов для Go

_«В сетях хаос — это норма. Пусть aoni будет твоим ледяным якорем»_

[![Go Version](https://img.shields.io/badge/go-1.27%2B-007d9c?logo=go&logoColor=white&style=flat-square)](https://go.dev/)
[![Go Reference](https://img.shields.io/badge/godoc-reference-007d9c?style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue?style=flat-square)](LICENSE)
[![Zero-Alloc](https://img.shields.io/badge/память-0%20B%2Fop%20%7C%200%20allocs-brightgreen?style=flat-square)](docs/CPU_STACK.md)
[![Chromium Grade](https://img.shields.io/badge/стабильность-Chromium--Grade-blueviolet?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)
[![Linux io_uring](https://img.shields.io/badge/linux-io__uring%202.34M%2B%20RPS-orange?style=flat-square)](netutil/iouring)
[![Security Invariants](https://img.shields.io/badge/безопасность-Fuzz%20%26%20Invariants-success?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)

<b>aoni</b> — сетевой стек и HTTP-клиент для Go. Поддерживает стандарты IETF RFC, спецификации W3C и механизмы устойчивости сетевого стека Chromium.

#### [English](README.md) • Русский • [Спецификация безопасности](docs/SECURITY_AND_FIDELITY.md) • [Руководство по Vortex](docs/VORTEX.md)

</div>

## Установка

`aoni` требует Go версии `1.27` или выше.

```bash
go get github.com/lemon4ksan/aoni
```

## Быстрый старт

Пример HTTP-запроса с типизацией ответа через дженерики:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	// 1. Инициализация клиента
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithChrome(), // Эмуляция TLS/JA4 и HTTP/2 профиля Chrome
	)

	// 2. GET-запрос с автоматическим декодированием ответа
	user, err := client.GetTo[User](ctx, "/users/{id}",
		mod.WithVar("id", 42),
		mod.WithHeader("Accept", "application/json"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Пользователь: %s (ID: %d)\n", user.Name, user.ID)
}
```

## Примеры использования

### 1. Generic-методы (`client.GetTo[T]`, `client.PostTo[T]` и др.)
Тело запроса и ответ сериализуются и десериализуются автоматически:

```go
// GET-запрос с декодированием ответа в *User
user, err := client.GetTo[User](ctx, "/users/42")

// POST с телом запроса
created, err := client.PostTo[User](ctx, "/users", User{Name: "Alice"})

// PUT, PATCH, DELETE
updated, err := client.PutTo[User](ctx, "/users/42", User{Name: "Alice Cooper"})
deleted, err := client.DeleteTo[User](ctx, "/users/42")

// Произвольный HTTP-метод
res, err := client.FetchTo[User](ctx, "CUSTOM", "/endpoint", payload)
```

### 2. Доступ к `*http.Response` и декодирование в существующую структуру

```go
// GetEx возвращает типизированный результат и сырой *http.Response
user, resp, err := client.GetEx[User](ctx, "/users/42")
fmt.Printf("Статус: %d, Сервер: %s\n", resp.StatusCode, resp.Header.Get("Server"))

// GetInto десериализует ответ в переданную структуру
var existing User
err := client.GetInto(ctx, "/users/42", &existing)
```

### 3. Построитель запросов (`client.R()`)
Для составных запросов с параметрами пути, query-параметрами и заголовками:

```go
var user User

resp, err := client.R().
	SetContext(ctx).
	SetPathParam("userId", "42").
	SetQueryParam("fields", "id,name,email").
	SetHeader("X-Trace-ID", "trace-12345").
	SetBody(map[string]any{"active": true}).
	SetResult(&user).
	Post("/users/{userId}/update")
```

### 4. Вызовы функций пакета (`aoni.GetTo[T]`, `aoni.PostTo[T]`)
Для выполнения запросов без создания отдельного экземпляра клиента:

```go
user, err := aoni.GetTo[User](ctx, "https://api.example.com/users/42")
```

### 5. Движок `fast.Client`
Реализация на базе `fasthttp` для сценариев с высокими требованиями к пропускной способности:

```go
import "github.com/lemon4ksan/aoni/fast"

fastClient := fast.NewClient(
	option.WithBaseURL("https://api.example.com"),
	option.WithChrome(),
)

user, err := fastClient.GetTo[User](ctx, "/users/42")
```

## ⚡ Сравнение производительности

Результаты микробенчмарков при параллельной нагрузке (12 ядер CPU, `b.RunParallel`):

| HTTP Клиент / Движок | RPS (12 ядер) | Аллокации | Память / op | HTTP/2 и HTTP/3 | Постквантовый TLS 1.3 | Профиль Chromium JA4 |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **`aoni/fast` (`io_uring`)** | **2 480 000+** | **0 allocs/op** | **0 B/op** | **✓ (H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓** |
| **`aoni.Client` (Stdlib)** | **640 000+** | **1 alloc/op** | **24 B/op** | **✓ (H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓** |
| `fasthttp` | 1 910 000 | 0 allocs/op | 0 B/op | ✗ (Нет H2/H3) | ✗ | ✗ |
| `net/http` (Stdlib) | 165 000 | 78 allocs/op | 6 800 B/op | ⚠️ (Только H2) | ✗ | ✗ |
| `go-resty/resty` | 142 000 | 86 allocs/op | 8 940 B/op | ✗ | ✗ | ✗ |

## Архитектура

### 1. Публичный API и транспорт
* **Стабильный интерфейс:** Базовые методы RFC 9110 (`client.GetTo[T]`, `client.PostTo[T]`, `client.Get`, `client.R()`, `option.With...`, `mod.With...`) зафиксированы в рамках v1.x.
* **Транспорт:** Поддерживает согласование протоколов (HTTP/1.1, HTTP/2, HTTP/3, TLS 1.3 с ML-KEM, MASQUE), алгоритм Happy Eyeballs и переиспользование буферов.
* **Расширения `aoni/x/...`:** Дополнительные интеграции и протокольные адаптеры (Socket.IO v5, GeoIP MMDB) вынесены в отдельные пакеты.

### 2. Безопасность памяти (Zero-Copy) и линтер Vortex
При интенсивном переиспользовании буферов (`sync.Pool`) случайная передача заимствованных срезов в фоновые горутины может приводить к race condition. Для проверки корректности работы со срезами в тулчейн включен статический анализатор `vortex check`:
* Проверяет, что заимствованные буферы не утекают в несинхронизированные фоновые горутины.
* Проверяет корректность непересекающихся срезов.
* Контролирует жизненный цикл выделенных ресурсов.

```bash
# Проверка в CI/CD:
vortex check --strict ./...
```

### 3. Оптимизация работы с CPU и памятью
1. **Буферы Per-P (`pool.PerPStorage`)**: Локальные для P буферы снижают конкуренцию за мьютексы при высокой многопоточной нагрузке.
2. **Off-Heap аллокатор (`offheap.SlabAllocator`)**: Размещение служебных структур протоколов вне кучи Go для снижения нагрузки на GC.
3. **Выравнивание кэш-линий (`_ cpu.CacheLinePad`)**: Выравнивание структур по границе 64 байт для предотвращения False Sharing.
4. **SIMD-поиск**: Векторизованный поиск разделителей `\r\n` (AVX2 / SWAR).
5. **Монотонные часы**: Снижение оверхеда системных вызовов при замере временных меток.
6. **Поддержка Linux `io_uring` (`netutil/iouring`)**: Использование очередей SQ/CQ для операций ввода-вывода.

## Инструмент Vortex

В состав `aoni` входит CLI-утилита **`vortex`** для генерации клиентов и mock-серверов по спецификациям OpenAPI 3.1, AsyncAPI 2.x/3.x и Protobuf:

```go
package userapi

import (
	"context"
	"github.com/lemon4ksan/aoni/mod"
)

// @aoni:service
// @base_url "https://api.example.com"
type UserAPI interface {
	// @get /users/{id}
	// @header "Accept: application/json"
	GetUser(ctx context.Context, id int, mods ...aoni.RequestModifier) (*User, error)

	// @post /users
	CreateUser(ctx context.Context, req CreateUserRequest, mods ...aoni.RequestModifier) (*User, error)
}
```

```bash
# Генерация кода клиентов
vortex gen

# Генерация in-memory mock-серверов для тестов
vortex mock

# Статическая проверка контрактов
vortex check --strict ./...
```

Подробнее см. в [**Руководстве по Vortex**](docs/VORTEX.md) и [**Спецификации Vortex**](docs/SPEC.md).

## Дополнительные протоколы и возможности

<details>
<summary><b>1. Protobuf и gRPC-Web (Unary и Streaming)</b></summary>

```go
// Вызов gRPC-Web с 5-байтным фреймингом и валидацией трейлеров
userResp, resp, err := aoni.PostGRPCWebTo[pb.UserResponse](ctx, client, "/UserService/GetUser", &pb.UserRequest{
	UserId: 42,
})
```

</details>

<details>
<summary><b>2. WebSockets поверх HTTP/2 Extended CONNECT (RFC 8441)</b></summary>

```go
import "github.com/lemon4ksan/aoni/realtime/ws"

conn, resp, err := ws.Dial(ctx, "wss://stream.example.com/feed",
	ws.WithH2ExtendedConnect(),
	ws.WithSubprotocols("graphql-transport-ws"),
)
if err != nil {
	panic(err)
}
defer conn.Close()

_ = conn.WriteText("{\"type\":\"subscribe\"}")
```

</details>

<details>
<summary><b>3. Постквантовый TLS 1.3 и Encrypted Client Hello (ECH / RFC 9460)</b></summary>

```go
client := aoni.NewClient(nil,
	option.WithPostQuantumKyber(),        // Гибридный обмен ключами X25519MLKEM768
	option.WithECH(option.ECHModeStrict), // Encrypted Client Hello через DoH/DoQ
	option.WithChrome(),                  // Эмуляция отпечатков JA4 / p0f
)
```

</details>

<details>
<summary><b>4. Happy Eyeballs v3 и Early Hints (RFC 8297)</b></summary>

```go
// Предварительное разрешение DNS и установка TLS-соединения
_ = client.Preconnect(ctx, "https://api.example.com")
_ = client.Preresolve(ctx, "api.example.com")
```

</details>

<details>
<summary><b>5. Защита конфиденциальных данных и 0-RTT Anti-Replay (RFC 8470)</b></summary>

```go
import "github.com/lemon4ksan/aoni/netutil/secret"

// Значения внутри secret.Secret маскируются в логах, JSON и стек-трейсах
token := secret.New("super-secret-api-token")

client := aoni.NewClient(nil,
	option.WithSecretBearer(token),
)
```

</details>

## Микробенчмарки

<details>
<summary><b>Детальные микробенчмарки подсистем (Нажмите, чтобы развернуть)</b></summary>

### 1. Микробенчмарки подсистем

| Подсистема / Операция | Стандартная библиотека Go / `x/...` | `aoni` | Разница по задержке | Память (`B / op`) |
| :--- | :---: | :---: | :---: | :---: |
| **Парсинг URL (`net/url.Parse`)** | 295.1 нс | **85.2 нс** (`net/url`) | **В 3.5 раза быстрее** | L1 кэш (CRC32) |
| **Public Suffix (`eTLD+1`)** | 146.3 нс | **78.8 нс** (`net/psl`) | **В 1.9 раза быстрее** | **0 B / 0 allocs** |
| **QPACK RFC 9204 кодек блоков** | 2 500+ нс (`quic-go/qpack`) | **472.7 нс** (`internal/fast/h3engine`) | **В 5.3 раза быстрее** | **0 B / 0 allocs** |
| **HPACK декодер полей** | 391.9 нс (`x/net/http2/hpack`) | **329.2 нс** (`internal/fast/h2engine`) | **В 1.19 раза быстрее** | **0 B / 0 allocs** |
| **HPACK Хаффман энкодер** | 18.5 нс | **13.99 нс** (`internal/fast/h2engine`) | **В 1.32 раза быстрее** | **0 B / 0 allocs** |
| **Таймстемпы (`vDSO` Bypass)** | 3.15 нс (`time.Now`) | **0.28 нс** (`silicon/clock`) | **В 11.2 раза быстрее** | **0 B / 0 allocs** |
| **Ограничитель Token Bucket** | 85+ нс (`x/time`) | **23.8 нс** (`async/rate`) | **В 3.6 раза быстрее** | **0 B / 0 allocs** |
| **SWAR UTF-8 сканирование (1KB)** | 280+ нс (`bytes.Index`) | **5.88 нс** (`silicon/simd`) | **12.4 ГБ/с** | **0 B / 0 allocs** |
| **SWAR `\r\n` сканирование (1KB)** | 280+ нс (`bytes.Index`) | **114.4 нс** (`silicon/simd`) | **В 2.5 раза быстрее** | **0 B / 0 allocs** |
| **Zstd декомпрессия (1KB)** | 1.8+ мкс (`klauspost/zstd`) | **251.6 нс / 0 B** (`compress/zstd`) | **В 7.2 раза быстрее** | **0 B / 0 allocs** |
| **Brotli декомпрессия (1KB)** | 2.1+ мкс (`google/brotli`) | **282.6 нс / 0 B** (`compress/brotli`) | **В 7.4 раза быстрее** | **0 B / 0 allocs** |
| **Пропускная способность WebSocket** | 800 МБ/с (`gorilla/websocket`) | **1 789 МБ/с** (`realtime/ws`) | **В 2.23 раза быстрее** | **0 B / 0 allocs** |

### 2. Векторизация Gollvm (LLVM 20.1.8 -O3)

| Нагрузка / Подсистема | Стандартный Go (`gc`) | Gollvm (`LLVM 20.1.8 -O3`) | Ускорение | Механизм |
| :--- | :---: | :---: | :---: | :--- |
| **ASCII Header Case-Folding и матчинг** | 8.47 нс/match | **1.71 нс/match** | **⚡ В 4.95 раза быстрее** | Векторизованное побитовое разворачивание без ветвлений |
| **HPACK / QPACK Хаффман-битовый кодек** | 324.32 МБ/с (464.6 нс) | **697.84 МБ/с (215.9 нс)** | **⚡ В 2.15 раза быстрее** | 64-битный регистровый сдвиг и упаковка |
| **QUIC / Protobuf Varint кодек** | 22.41 нс/op | **15.19 нс/op** | **⚡ В 1.48 раза быстрее** | Развернутое извлечение битовых масок |
| **EWMA фильтр задержки и джиттера** | 2.74 нс/sample | **1.92 нс/sample** | **⚡ В 1.43 раза быстрее** | Конвейеризация float-регистров и FMA |

</details>

## Поддерживаемые протоколы и возможности

| Возможность / Архитектурный уровень | Go `net/http` | Обертки (напр., Resty) | Движок `aoni` |
| :--- | :---: | :---: | :---: |
| **Статический анализ буферов (`vortex lint`)** | ✗ | ✗ | **✓ (CFG и Escape-анализ)** |
| **Конкуренция за аллокатор на многоядерных CPU** | ⚠️ (`sync.Pool`) | ⚠️ (Высокая конкуренция) | **✓ (`pool.PerPStorage` с привязкой к ядрам)** |
| **Linux `io_uring`** | ✗ | ✗ | **✓ (Разделяемые SQ/CQ буферы)** |
| **Снижение нагрузки GC на служебные структуры** | ✗ (Аллокации в куче) | ✗ (Аллокации в куче) | **✓ (`offheap.SlabAllocator`)** |
| **Нативный HTTP/2 Мультиплексор** | ⚠️ (`x/net/http2`) | ✗ | **✓ (Нативный движок и LUT)** |
| **Нативный HTTP/3 / QUIC / QPACK** | ✗ | ✗ | **✓ (RFC 9000 и RFC 9204)** |
| **Постквантовый обмен ключами TLS 1.3** | ✗ | ✗ | **✓ (X25519MLKEM768 и Zstd сжатие сертификатов)** |
| **RFC 8297 `103 Early Hints`** | ✗ | ✗ | **✓ (Парсинг Link и преконнект сокетов)** |
| **Network Isolation (NIK)** | ✗ | ✗ | **✓ (Ключи TopFrame/FrameSite и куки CHIPS)** |
| **Приоритеты RFC 9218 (Extensible Priorities)** | ✗ | ✗ | **✓ (Приоритеты стримов `u=0..7, i`)** |
| **RFC 8767 Stale-While-Revalidate DNS** | ✗ | ✗ | **✓ (Stale DNS с фоновым обновлением)** |
| **Словари сжатия RFC 9651 (Compression Dicts)**| ✗ | ✗ | **✓ (`dcb`, `dcz` и `Sec-Available-Dictionary`)** |
| **Декодирование через дженерики** | ✗ (Вручную) | ✗ (Через рефлексию `any`) | **✓ (Типобезопасное `[T]`)** |
| **gRPC и gRPC-Web (4 режима стриминга)** | ✗ | ✗ | **✓ (Unary, Server, Client и Bidi)** |
| **Chromium Happy Eyeballs v3** | ⚠️ (Только IPv4/v6) | ✗ | **✓ (Гонка протоколов H3 / H2 / H1)** |
| **Автоматическое восстановление соединений** | ✗ | ✗ | **✓ (HTTP 421, 408, 425 и динамический Alt-Svc Backoff)** |
| **W3C `No-Vary-Search` кэширование** | ✗ | ✗ | **✓ (Нормализация Query-параметров)** |
| **TLS 1.3 Encrypted Client Hello** | ✗ | ✗ | **✓ (ECH / RFC 9460 через DoH/DoQ)** |
| **Защита учетных данных и 0-RTT Anti-Replay** | ✗ | ✗ | **✓ (`Secret[T]` и RFC 8470)** |
| **Изолированный движок PAC / WPAD** | ✗ | ✗ | **✓ (Парсер и исполнитель PAC)** |
| **Обработка сна ОС** | ✗ | ✗ | **✓ (Очистка невалидных сокетов при смене состояния сети)** |
| **Circuit Breaking** | ✗ | ✗ | **✓ (EWMA и расчёт процента ошибок)** |
| **Парсинг `Retry-After`** | ✗ | ✗ | **✓ (Delta-sec и RFC 1123 datetime)** |
| **Декодирование нестандартных кодировок** | ✗ | ✗ | **✓ (Автоматический движок WhatWG Encoding)** |
| **Эмуляция TLS (JA3/JA4/JA4H/p0f)** | ✗ | ✗ | **✓ (Профили Chrome, Firefox, Safari)** |
| **Unix Domain Sockets** | ⚠️ (Вручную) | ✗ | **✓ (Схема `unix://`)** |
| **L3/L4 & MASQUE Туннели** | ✗ | ✗ | **✓ (Wintun, Darwin utun, /dev/net/tun, MASQUE RFC 9298)** |
| **OpenTelemetry и W3C Трейсинг** | ✗ | ✗ | **✓ (`aoni/x/otel` без внешних зависимостей)** |
| **Клиент Socket.IO / Engine.IO v4** | ✗ | ✗ | **✓ (`aoni/x/socketio`)** |
| **Изоляция сессий и кук по прокси** | ✗ | ✗ | **✓ (`ProxyIsolatedJar` RFC 6265)** |

## 📦 Структура репозитория

```
aoni/
├── option/       // Опции инициализации клиента (option.With...)
├── mod/          // Модификаторы запросов (mod.With...)
├── fast/         // Движок высокой производительности на базе fasthttp
├── tunnel/       // L3/L4 туннелирование: SSH Jump Hosts & Reverse Gateway, MASQUE (RFC 9298), Wintun L3
├── cookie/       // Прокси-изолированные куки, формат Netscape, сортировка по RFC 6265
├── fingerprint/  // Профили TLS/JA4/p0f, заголовки HTTP/2, паддинг
├── netutil/      // Ротатор прокси, DoH/DoT DNS резолверы, PAC движок, NIK, Priority, Early Hints
├── codec/        // Декодеры ответов (JSON, Proto, gRPC-Web, XML) и кодирование параметров
├── realtime/     // WebSocket поверх H2 CONNECT (RFC 8441), SSE и NDJSON потоки
├── resiliency/   // Локальное кэширование, обработка WAF-челленджей, балансировщики
├── telemetry/    // Генераторы HAR, трекеры задержки EWMA, хуки трассировки и cURL-экспортеры
└── x/            // Расширения и адаптеры протоколов (x/otel, x/socketio, x/geoip)
```

## Примеры и интеграции

- [ao](https://github.com/Lemon4ksan/ao): Форк `curl`, чей сетевой транспорт HTTP/HTTPS/WS полностью переведён на движок `libaoni` (`lib/aoni_bridge.c`).
  - Замена сетевого ядра позволила разогнать оригинальный Си-код: `ao` выдаёт **9 145+ RPS** на 100 параллельных POSIX-потоках (в 3–5 раз быстрее стандартного многопоточного `curl`), добавляя поддержку отпечатков Chromium uTLS (JA4) и постквантового TLS 1.3 (ML-KEM-768) без утечек памяти.
- [discordgo-aoni](https://github.com/lemon4ksan/discordgo-aoni): Форк библиотеки `discordgo` с сетевым транспортом на базе `aoni` и `aoni/realtime/ws`, оптимизированный для снижения аллокаций при обработке REST и WebSocket.

## 📚 Техническая документация

- [**Инварианты безопасности и протоколов**](docs/SECURITY_AND_FIDELITY.md): Модель защиты, фильтрация SSRF, DNS rebinding, защита от декомпрессионных бомб.
- [**Руководство по Vortex**](docs/VORTEX.md): Декларативный синтаксис, OpenAPI/AsyncAPI парсинг, mock-серверы и интеграция с CI/CD.
- [**Спецификация Vortex DSL**](docs/SPEC.md): Грамматика EBNF и правила статического линтера.
- [**Спецификация сетевого стека**](docs/NETWORK_STACK.md): Happy Eyeballs v3, восстановление после HTTP 421/408/425, ECH и управление пулами соединений.
- [**Оптимизация CPU и памяти**](docs/CPU_STACK.md): SIMD-операции, slab-аллокация и распределение памяти.
- [**Низкоуровневые механизмы**](docs/VOODOO.md): Управление состоянием HPACK, системные вызовы для сокетов и фрейминг.
- [**Книга рецептов (Cookbook)**](docs/COOKBOOK.md): Практические примеры для REST, WebSockets, gRPC-Web и стриминга.
- [**Примеры кода**](examples): Примеры использования библиотеки.

## 🧾 Лицензия

Лицензировано под **BSD 3-Clause License**. Подробности в файле [LICENSE](LICENSE).
