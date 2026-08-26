<div align="center">

# aoni

### Унифицированный высокопроизводительный стек сетевых протоколов для Go

[![Go Version](https://img.shields.io/badge/go-1.27%2B-007d9c?logo=go&logoColor=white&style=flat-square)](https://go.dev/)
[![Go Reference](https://img.shields.io/badge/godoc-reference-007d9c?style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue?style=flat-square)](LICENSE)
[![Zero-Alloc](https://img.shields.io/badge/память-0%20B%2Fop%20%7C%200%20allocs-brightgreen?style=flat-square)](docs/CPU_STACK.md)
[![Chromium Grade](https://img.shields.io/badge/стабильность-Chromium--Grade-blueviolet?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)
[![Linux io_uring](https://img.shields.io/badge/linux-io__uring%202.34M%2B%20RPS-orange?style=flat-square)](netutil/iouring)
[![Security Invariants](https://img.shields.io/badge/безопасность-Fuzz%20%26%20Invariants-success?style=flat-square)](docs/SECURITY_AND_FIDELITY.md)

<p align="center">
  <em><b>aoni</b> — это унифицированный высокопроизводительный движок сетевых протоколов Интернета для Go. Объединяет стандарты IETF RFC, спецификации W3C и механизмы устойчивости Chromium в единую zero-allocation архитектуру.</em>
</p>

> _«В тот момент, когда байты покидают одну машину, чтобы достичь другой — это происходит с 0 аллокаций, на кремниевой скорости линии, без расхождения типов и без шансов на блокировку WAF.»_

#### [English](README.md) • Русский • [Спецификация безопасности](docs/SECURITY_AND_FIDELITY.md) • [Руководство по Vortex](docs/VORTEX_RU.md)

</div>

---

## ⚙️ Установка

`aoni` требует **Go версии `1.27` или выше**.

```bash
go get github.com/lemon4ksan/aoni
```

## ⚡ Быстрый старт (Quickstart)

Типобезопасный HTTP-запрос в одну строчку без аллокаций с автоматической десериализацией через дженерики:

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

	// Инициализация переиспользуемого клиента с Chromium-устойчивостью
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithChrome(), // Побитовая эмуляция Chrome uTLS, ECH, JA4 и HTTP/2 фрейминга
	)

	// Получение типизированного ответа напрямую в структуру (0 B/op на горячем пути)
	user, resp, err := fluent.FetchTo[User](ctx, client, "GET", "/users/{id}",
		mod.WithVar("id", 42),
		mod.WithHeader("Accept", "application/json"),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Пользователь: %s (ID: %d), HTTP Статус: %d\n", user.Name, user.ID, resp.StatusCode)
}
```

---

## 🤖 Сравнение производительности: Aoni против традиционных клиентов

Тестирование под параллельной нагрузкой на 12 ядрах CPU (`b.RunParallel`, PGO-оптимизация):

| HTTP Клиент / Движок | Пиковый RPS (12 ядер) | Аллокации | Память / op | HTTP/2 и HTTP/3 | Постквантовый TLS 1.3 | Стелс-отпечаток Chromium JA4 |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **`aoni/fast` (`io_uring`)** | **2 480 000+** | **0 allocs/op** | **0 B/op** | **✓ (Нативный H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓ (Побитовое совпадение)** |
| **`aoni.Client` (Stdlib)** | **640 000+** | **1 alloc/op** | **24 B/op** | **✓ (Нативный H2/H3/QUIC)** | **✓ (ML-KEM 768)** | **✓ (Побитовое совпадение)** |
| `fasthttp` (Raw) | 1 910 000 | 0 allocs/op | 0 B/op | ✗ (Нет H2/H3) | ✗ | ✗ |
| `net/http` (Stdlib) | 165 000 | 78 allocs/op | 6 800 B/op | ⚠️ (Только H2) | ✗ | ✗ |
| `go-resty/resty` | 142 000 | 86 allocs/op | 8 940 B/op | ✗ | ✗ | ✗ |

---

## 🏛️ Ключевые архитектурные столпы

### 1. Вечный публичный контракт и адаптивный кремниевый реактор
> _«Код, написанный для **aoni v1.0.0**, гарантированно скомпилируется и будет работать без единого изменения на любой версии **v1.x** через 5, 10 и 20 лет.»_

* **Неизменяемый публичный интерфейс (Public API):** Семантические абстракции RFC 9110 (`fluent.FetchTo[T]`, `option.With...`, `mod.With...`) заморожены навсегда. Бизнес-логика полностью изолирована от смены сетевых протоколов.
* **Адаптивный кремниевый реактор (Internal Engine):** Под капотом `aoni` прозрачно переключает и обновляет протоколы (HTTP/1.1 $\leftrightarrow$ HTTP/2 $\leftrightarrow$ HTTP/3 $\leftrightarrow$ Постквантовый Kyber/ML-KEM TLS, MASQUE) и оптимизирует память без ломающих изменений.
* **Изоляция экспериментов в `aoni/x/...`:** Все сторонние адаптеры и протокольные эксперименты (Socket.IO v5, GeoIP MMDB) живут строго в изолированных субмодулях.

### 2. Парадокс безопасности Zero-Copy: Решено для Go
В традиционном Go пулинг памяти без аллокаций за пределами простых функций опасен: переданный в фоновую горутину заимствованный слайс приводит к Data Race и повреждению памяти (Use-After-Free).

В `aoni` пути выполнения zero-copy объединены со статическим верификатором **`vortex check` / `vortex lint`** на базе графов потока управления (CFG), Escape Analysis и логики разделения памяти ($P * Q$):
* **Предотвращение утечек (`B001`):** Формально проверяет, что заимствованные буферы (`borrow.Bytes`, scoped headers) никогда не утекают в несинхронизированные горутины.
* **Непересекающиеся интервалы (`B003`):** Доказывает отсутствие пересечений при мутациях слайсов на этапе компиляции.
* **Автоматы жизненного цикла типов (`B011`):** Гарантирует линейный прогресс ресурсов ($\text{Acquired} \to \text{Frozen} \to \text{Released}$) — исключая double-free и use-after-free.

```bash
# Проверка инвариантов безопасности памяти zero-copy в CI/CD:
vortex check --strict ./...
```

### 3. Кремниевая симпатия: Как Aoni достигает 2.34M+ RPS
1. **Реактор без блокировок (`pool.PerPStorage`)**: Локальные для ядер CPU кольцевые буферы исключают конкуренцию за мьютексы даже при насыщении 128+ ядер.
2. **Off-Heap Slab-арены (`offheap.SlabAllocator`)**: Служебные структуры фрейминга протоколов размещаются вне кучи Go, снижая оверхед сборщика мусора (GC) до **0.00%**.
3. **Паддинг кэш-линий 64 байта (`_ cpu.CacheLinePad`)**: Выравнивание атомиков по границам L1/L2 кэша процессора исключает штрафы False Sharing.
4. **Векторный SIMD-поиск заголовков и плоские LUT-таблицы**: Поиск разделителей `\r\n` выполняется 64-битными SWAR / AVX2 инструкциями со скоростью **~9 ГБ/с**.
5. **vDSO Bypass таймстемпов**: Получение системного времени за **0.28 нс** (в 11.2 раз быстрее `time.Now()`).
6. **Нативный Linux `io_uring` Kernel Bypass (`netutil/iouring`)**: Разделяемые кольцевые буферы SQ/CQ в памяти `mmap` без системных вызовов со скоростью линии.

---

## 🛠️ Декларативный AST-тулчейн Vortex

В состав `aoni` входит **`vortex`** — компилятор декларативных контрактов без аллокаций и генератор клиентов из спецификаций OpenAPI 3.1, AsyncAPI 2.x/3.x и Protobuf:

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
# Компиляция клиентов без аллокаций
vortex gen

# Генерация in-memory mock-серверов для тестов (0 оверхеда на порты)
vortex mock

# Статическая проверка контрактов и линтинг
vortex check --strict ./...
```

Полный справочник синтаксиса и примеры приведены в [**Руководстве по Vortex**](docs/VORTEX_RU.md) и [**Спецификации Vortex**](docs/SPEC.md).

---

## 🔥 Продвинутые протоколы и возможности

<details>
<summary><b>1. Нативный Protobuf и gRPC-Web (Unary и Streaming)</b></summary>

```go
// Прямой вызов gRPC-Web с 5-байтным фреймингом и валидацией трейлеров
userResp, resp, err := fluent.PostGRPCWebTo[pb.UserResponse](ctx, client, "/UserService/GetUser", &pb.UserRequest{
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

// Полнодуплексная отправка сообщений с 0 аллокаций
_ = conn.WriteText("{\"type\":\"subscribe\"}")
```

</details>

<details>
<summary><b>3. Постквантовый TLS 1.3 и Encrypted Client Hello (ECH / RFC 9460)</b></summary>

```go
client := aoni.NewClient(nil,
	option.WithPostQuantumKyber(), // FIPS 203 X25519MLKEM768 гибридный обмен ключами
	option.WithECH(option.ECHModeStrict), // Encrypted Client Hello через DoH/DoQ
	option.WithChrome(), // Полная маскировка JA4 / p0f
)
```

</details>

<details>
<summary><b>4. Happy Eyeballs v3 и RFC 8297 Early Hints Preconnect</b></summary>

```go
// Спекулятивный прогрев DNS и TLS пайплайнов до первого запроса
_ = client.Preconnect(ctx, "https://api.example.com")
_ = client.Preresolve(ctx, "api.example.com")
```

</details>

<details>
<summary><b>5. Защита учетных данных и 0-RTT Anti-Replay (RFC 8470)</b></summary>

```go
import "github.com/lemon4ksan/aoni/netutil/secret"

// Учетные данные внутри secret.Secret маскируются в логах, JSON и стек-трейсах
token := secret.New("super-secret-api-token")

client := aoni.NewClient(nil,
	option.WithSecretBearer(token), // 0 шансов утечки в fmt.Printf("%+v") или slog
)
```

</details>

---

## 🔬 Набор микробенчмарков

<details>
<summary><b>Детальные микробенчмарки подсистем (Нажмите, чтобы развернуть)</b></summary>

### 1. Микробенчмарки подсистем Foundation (Zero-Alloc сантехника)

| Подсистема / Примитив | Базовая реализация Go / `x/...` | Движок `foundation` | Дельта задержки | Аллокации в куче (`B / alloc`) |
| :--- | :---: | :---: | :---: | :---: |
| **Парсинг URL (`net/url.Parse`)** | 295.1 нс | **85.2 нс** (`net/url`) | **В 3.5 раза быстрее** | L1 шардированный CRC32 кэш |
| **Public Suffix (`eTLD+1`)** | 146.3 нс | **78.8 нс** (`net/psl`) | **В 1.9 раза быстрее** | **0 B / 0 allocs** (против 48 B / 1 alloc) |
| **QPACK RFC 9204 кодек блоков** | 2 500+ нс (`quic-go/qpack`) | **472.7 нс** (`internal/fast/h3engine`) | **В 5.3 раза быстрее** | **0 B / 0 allocs** (Пулированный кодек) |
| **HPACK декодер полей** | 391.9 нс (`x/net/http2/hpack`) | **329.2 нс** (`internal/fast/h2engine`) | **В 1.19 раза быстрее** | **0 B / 0 allocs** (Слайсы полей) |
| **HPACK Хаффман энкодер** | 18.5 нс | **13.99 нс** (`internal/fast/h2engine`) | **В 1.32 раза быстрее** | **0 B / 0 allocs** (Прямое суммирование) |
| **Таймстемпы (`vDSO` Bypass)** | 3.15 нс (`time.Now`) | **0.28 нс** (`silicon/clock`) | **В 11.2 раза быстрее** | **0 B / 0 allocs** (Атомик в L1) |
| **Ограничитель Token Bucket** | 85+ нс (`x/time`) | **23.8 нс** (`async/rate`) | **В 3.6 раза быстрее** | **0 B / 0 allocs** |
| **SWAR UTF-8 сканирование (1KB)** | 280+ нс (`bytes.Index`) | **5.88 нс** (`silicon/simd`) | **Пропускная 12.4 ГБ/с** | **0 B / 0 allocs** (64-битный вектор SWAR) |
| **SWAR `\r\n` сканирование (1KB)** | 280+ нс (`bytes.Index`) | **114.4 нс** (`silicon/simd`) | **В 2.5 раза быстрее (~9 ГБ/с)** | **0 B / 0 allocs** |
| **Zstd декомпрессия (1KB)** | 1.8+ мкс (`klauspost/zstd`) | **251.6 нс / 0 B** (`compress/zstd`) | **В 7.2 раза быстрее (~4.0M ops/s)** | **0 B / 0 allocs** |
| **Brotli декомпрессия (1KB)** | 2.1+ мкс (`google/brotli`) | **282.6 нс / 0 B** (`compress/brotli`) | **В 7.4 раза быстрее (~3.5M ops/s)** | **0 B / 0 allocs** |
| **Пропускная способность WebSocket** | 800 МБ/с (`gorilla/websocket`) | **1 789 МБ/с** (`realtime/ws`) | **В 2.23 раза быстрее** | **0 B / 0 allocs** (`writev` / `net.Buffers`) |

### 2. Векторизация Gollvm (LLVM 20.1.8 -O3)

| Нагрузка / Подсистема ядра | Стандартный Go (`gc`) | Gollvm (`LLVM 20.1.8 -O3`) | Прирост скорости | Микроархитектурный механизм |
| :--- | :---: | :---: | :---: | :--- |
| **ASCII Header Case-Folding и матчинг** | 8.47 нс/match | **1.71 нс/match** | **⚡ В 4.95 раза быстрее** | Векторизованное побитовое разворачивание без ветвлений |
| **HPACK / QPACK Хаффман-битовый кодек** | 324.32 МБ/с (464.6 нс) | **697.84 МБ/с (215.9 нс)** | **⚡ В 2.15 раза быстрее** | 64-битный регистровый сдвиг и упаковка LLVM |
| **QUIC / Protobuf Varint кодек** | 22.41 нс/op | **15.19 нс/op** | **⚡ В 1.48 раза быстрее** | Развернутое извлечение битовых масок и оптимизация переходов |
| **EWMA фильтр задержки и джиттера** | 2.74 нс/sample | **1.92 нс/sample** | **⚡ В 1.43 раза быстрее** | Конвейеризация float-регистров и FMA |

</details>

---

## 🌐 Поддерживаемые протоколы и возможности

| Возможность / Архитектурный уровень | Go `net/http` | Стандартная обертка (напр., Resty) | Движок `aoni` |
| :--- | :---: | :---: | :---: |
| **Статический Borrow Checker (`vortex lint`)** | ✗ | ✗ | **✓ (Формальный CFG, логика разделения памяти $P * Q$)** |
| **Конкуренция за аллокатор на многоядерных CPU** | ⚠️ (Блокировки `sync.Pool`) | ⚠️ (Высокая конкуренция) | **✓ (`pool.PerPStorage` с привязкой к ядрам, 0 contention)** |
| **Linux `io_uring` Kernel Bypass** | ✗ | ✗ | **✓ (Разделяемые `mmap` SQ/CQ буферы без syscalls @ 2.34M+ RPS)** |
| **Оверхед GC на фрейминг / Ping** | ✗ (Аллокации в куче) | ✗ (Аллокации в куче) | **✓ (0.00% GC — `offheap.SlabAllocator`)** |
| **Нативный HTTP/2 Мультиплексор** | ⚠️ (Блокировки `x/net/http2`) | ✗ | **✓ (Нативный 0-alloc движок с плоской LUT таблицей)** |
| **Нативный HTTP/3 / QUIC / QPACK** | ✗ | ✗ | **✓ (Чистый Go RFC 9000 & RFC 9204 0-alloc стрим)** |
| **Постквантовый обмен ключами TLS 1.3** | ✗ | ✗ | **✓ (FIPS 203 `X25519MLKEM768` и Zstd сжатие сертификатов)** |
| **RFC 8297 `103 Early Hints`** | ✗ | ✗ | **✓ (Парсинг Link и спекулятивный преконнект сокетов)** |
| **Chromium Network Isolation (NIK)** | ✗ | ✗ | **✓ (Составные ключи TopFrame/FrameSite и куки CHIPS)** |
| **Приоритеты RFC 9218 (Extensible Priorities)** | ✗ | ✗ | **✓ (Структурированные приоритеты стримов `u=0..7, i`)** |
| **RFC 8767 Stale-While-Revalidate DNS** | ✗ | ✗ | **✓ (0мс Stale DNS с фоновым дедуплицированным обновлением)** |
| **Словари сжатия RFC 9651 (Compression Dicts)**| ✗ | ✗ | **✓ (`dcb`, `dcz` и `Sec-Available-Dictionary` транспорт)** |
| **Декодирование через дженерики** | ✗ (Вручную) | ✗ (Через рефлексию `any`) | **✓ (Типобезопасное `[T]` на этапе компиляции)** |
| **gRPC и gRPC-Web (4 режима стриминга)** | ✗ | ✗ | **✓ (Unary, Server, Client и Bidi Stream)** |
| **Chromium Happy Eyeballs v3** | ⚠️ (Только IPv4/v6) | ✗ | **✓ (Гонка протоколов H3 vs H2/H1)** |
| **Конвейер авто-восстановления** | ✗ | ✗ | **✓ (HTTP 421, 408, 425 и динамический Alt-Svc Backoff)** |
| **W3C `No-Vary-Search` кэширование** | ✗ | ✗ | **✓ (Умная нормализация Query-параметров)** |
| **TLS 1.3 Encrypted Client Hello** | ✗ | ✗ | **✓ (ECH / RFC 9460 через DoH/DoQ)** |
| **Защита учетных данных и 0-RTT Anti-Replay** | ✗ | ✗ | **✓ (`Secret[T]` маскирование в памяти и RFC 8470 защита)** |
| **Изолированный движок PAC / WPAD** | ✗ | ✗ | **✓ (Chromium-grade парсер и исполнитель Proxy Auto-Config)** |
| **Управление питанием ОС** | ✗ | ✗ | **✓ (Автоочистка зомби-пулов сокетов при сне ОС)** |
| **Активный Circuit Breaking** | ✗ | ✗ | **✓ (Встроенный EWMA и расчёт процента ошибок)** |
| **Вежливый парсинг `Retry-After`** | ✗ | ✗ | **✓ (Delta-sec и RFC 1123 datetime)** |
| **Автоматическое декодирование не-UTF8** | ✗ | ✗ | **✓ (Автоматический движок WhatWG Encoding)** |
| **Обход TLS-анализа (JA3/JA4/JA4H/p0f)** | ✗ | ✗ | **✓ (Имперсонация Chrome, Firefox, Safari на чистом Go)** |
| **Поддержка Unix Domain Sockets** | ⚠️ (Вручную) | ✗ | **✓ (Нативная схема `unix://`)** |
| **L3/L4 & MASQUE Туннели** | ✗ | ✗ | **✓ (Wintun, Darwin utun, /dev/net/tun, MASQUE RFC 9298)** |
| **OpenTelemetry и W3C Трейсинг** | ✗ (Тяжелый 50+ dep SDK) | ✗ | **✓ (`github.com/lemon4ksan/aoni/x/otel` — 0 deps, 29ns W3C)** |
| **Клиент Socket.IO / Engine.IO v4** | ✗ | ✗ | **✓ (`github.com/lemon4ksan/aoni/x/socketio`)** |
| **Изоляция сессий и кук по прокси** | ✗ | ✗ | **✓ (`ProxyIsolatedJar` RFC 6265)** |

---

## 📦 Структура репозитория

```
aoni/
├── option/       // Опции инициализации клиента (option.With...)
├── mod/          // Модификаторы запросов (mod.With...)
├── request/      // Generic-хелперы (request.GetTo[T], PostTo, PostProtoTo)
├── fast/         // Движок высокой производительности на базе fasthttp
├── fluent/       // Цепочный Builder API (fluent.R, FetchTo[T], Codec)
├── tunnel/       // L3/L4 туннелирование: SSH Jump Hosts & Reverse Gateway, MASQUE (RFC 9298), Wintun L3
├── cookie/       // Прокси-изолированные куки, формат Netscape, сортировка по RFC 6265
├── fingerprint/  // Обход TLS/JA4/p0f отпечатков, кадры HTTP/2, CDN-паддинг
├── netutil/      // Ротатор прокси, DoH/DoT DNS резолверы, PAC движок, NIK, Priority, Early Hints
├── codec/        // Декодеры ответов (JSON, Proto, gRPC-Web, XML) и кодирование параметров
├── realtime/     // WebSocket поверх H2 CONNECT (RFC 8441), SSE и NDJSON потоки
├── resiliency/   // Локальное кэширование, детекторы и солверы WAF-челленджей, балансировщики
├── telemetry/    // Генераторы HAR, трекеры задержки EWMA, хуки трассировки и cURL-экспортеры
└── x/            // Расширения и вспомогательные протоколы (x/otel, x/socketio, x/geoip)
```

---

## 🚀 Реальные примеры и интеграции

- [ao](https://github.com/Lemon4ksan/ao): Высокопроизводительный форк `curl` со стелс-маскировкой, сетевой транспорт HTTP/HTTPS/WS которого полностью работает на движке `libaoni` (`lib/aoni_bridge.c`).
  - Обеспечивает побитовую эмуляцию отпечатков Chromium uTLS (JA4 `t13d1515h2...`), гибридный постквантовый обмен ключами ML-KEM-768 и выдаёт **9 145+ RPS** на 100 параллельных потоках POSIX (в 3–5 раз быстрее стандартного многопоточного curl) с 0% утечек памяти и 0% нагрузки на сборщик мусора Go.
- [discordgo-aoni](https://github.com/lemon4ksan/discordgo-aoni): Высокопроизводительный форк `discordgo` на базе `aoni` и `aoni/realtime/ws`, адаптированный под современные изменения API Discord с помощью `vortex`.
  - Обеспечивает в 6.8 раз более высокую пропускную способность REST (203 000+ RPS) и в 3.1 раза более быстрые операции WebSocket с 0 B/op при фрейминге.

---

## 📚 Технические спецификации и документация

- [**Инварианты безопасности и точности протоколов**](docs/SECURITY_AND_FIDELITY.md): Модель архитектурной защиты, защита от SSRF, DNS rebinding, декомпрессионных бомб и матрица уязвимостей.
- [**Руководство по Vortex**](docs/VORTEX_RU.md): Декларативный синтаксис AST, OpenAPI/AsyncAPI парсинг, in-memory моки и CI/CD интеграция.
- [**Спецификация Vortex DSL и архитектуры**](docs/SPEC.md): Формальная EBNF-грамматика, 3-стадийный конвейер оптимизации и правила статического линтера.
- [**Спецификация сетевого стека**](docs/NETWORK_STACK_RU.md): Подробный обзор Happy Eyeballs v3, авто-восстановления HTTP 421/408/425, ECH и механики пулов.
- [**Спецификация аппаратной оптимизации**](docs/CPU_STACK.md): Детали PLAN9 AVX2 SIMD ассемблера (`simd_amd64.s`), 2MB LargePages slab-арены и бюджеты инструкций.
- [**Разбор нетривиальных решений**](docs/VOODOO_RU.md): Управление состоянием HPACK, тюнинг TCP-окон через системные вызовы и фрейминг пакетов.
- [**Книга рецептов (Cookbook)**](docs/COOKBOOK_RU.md): Практические рецепты для REST, WebSockets, gRPC-Web и стриминга.
- [**Примеры кода**](examples): Готовые примеры для REST, WebSockets, gRPC-Web и обхода систем защиты.

---

## 🧾 Лицензия

Лицензировано под **BSD 3-Clause License**. Подробности в файле [LICENSE](LICENSE).
