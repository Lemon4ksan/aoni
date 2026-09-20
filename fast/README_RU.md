<div align="center">

# aoni/fast

### Zero-Alloc сетевой движок для Go

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/fast)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
[![Throughput](https://img.shields.io/badge/throughput-193k%20H1%20%7C%2070k%20H2%20%7C%202.12M%20In--Memory-brightgreen?style=flat-square)](#эмпирические-бенчмарки-и-разбор-протоколов)

> _"Строгая геометрия памяти. Чистая скорость железа."_

#### [English](README.md) • Русский

</div>

## Манифест: Против раздутого софта

Годами фреймворки внушали ленивую догму:  
> *"Хочешь удобный интерфейс с цепочками методов — плати 50 микросекунд задержки и 80 аллокаций в куче на каждый чих. А если нужна скорость — пиши нечитаемый спагетти-код на голых указателях."*

Это ерунда и оправдание лени.

`aoni/fast` берет `mach`, прикручивает нативный фрейминг HTTP/2 и HTTP/3 прямо поверх uTLS и упаковывает всё это в единый лаконичный интерфейс option/mod из `aoni`. 

Использовать стандартные жирные HTTP-обертки — это как нанять толпу из пятидесяти пьяных грузчиков, которые со скрежетом тащат один бумажный конверт через весь город, месят грязь и требуют цистерну бензина. `aoni/fast` — это прямоточная пневматическая труба: загружаешь байты в сокет, дергаешь рычаг, и они вылетают на провод без единой лишней аллокации на полу.

```shell
go get github.com/lemon4ksan/aoni
```

## Матрица возможностей

| Возможность / Фича | Стандартный `net/http` | Resty / Обертки | `aoni` (Базовый) | `aoni/fast` |
| :--- | :---: | :---: | :---: | :---: |
| **Ядро движка** | `net/http` | `net/http` | `net/http` | **`mach` + Нативный H2/H3** |
| **HTTP/1.1 Пропускная способность (Parallel)** | ~17.3k RPS | ~16k RPS | ~17k RPS | **151k – 193k RPS (8.7x – 11x)** |
| **HTTP/1.1 Задержка** | 57.6 мкс | ~60 мкс | 56 мкс | **6.6 мкс (в 8.7 раз быстрее)** |
| **HTTP/2 Пропускная способность (TLS)** | 43.4k RPS | ~40k RPS | ~40k RPS | **60.5k – 69.3k RPS** |
| **HTTP/2 Задержка (TLS)** | 23.0 мкс | ~25 мкс | ~24 мкс | **16.5 мкс (в 1.4 раза быстрее)** |
| **HTTP/3 QUIC Пропускная способность** | N/A | N/A | N/A | **1 091 RPS** |
| **In-Memory Core (Без I/O)** | N/A | N/A | N/A | **2 126 754 RPS (0.47 мкс)** |
| **Пулинг объектов (Zero-Alloc)** | ✗ (73-83 аллокации) | ✗ (>90 аллокаций) | ✗ | **✓ (`PerPStorage` Request/Response)** |
| **Нативный HTTP/2 (`h2`)** | `x/net/http2` | `x/net/http2` | `x/net/http2` | **✓ (`mach/client/h2` Singleflight)** |
| **Нативный HTTP/3 (`h3`)** | `quic-go` | `quic-go` | `quic-go` | **✓ (`mach/client/h3` + QPACK)** |
| **uTLS и отпечатки** | ✗ | ✗ | **✓** | **✓ (uTLS поверх `fastDialer`)** |
| **Кастомный порядок заголовков (JA4H)** | ✗ | ✗ | **✓** | **✓** |
| **Мост совместимости с `http.Client`** | Нативно | ✗ | Нативно | **✓ (`fast.NewStdClient`)** |

## Эмпирические бенчмарки и разбор протоколов

Замеры проводились на **Intel Core i5-12400F @ 4.4 GHz (6 ядер / 12 потоков)**, Windows amd64, Go 1.27 (`go test -bench=BenchmarkFast_ -benchmem ./tests`). Использовались реальные сетевые loopback-сокеты (TCP / TLS / UDP QUIC).

### 1. Сравнение производительности протоколов (Параллельно, 12 воркеров)

| Протокол / Стек клиента | Запросов в сек (**RPS**) | Задержка (`ns/op`) | Память (`B/op`) | Аллокации (`allocs/op`) | Ускорение относительно `net/http` |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **`aoni/fast` (In-Memory Core)** | **2 126 754 RPS** | **0.47 мкс** (470 ns) | 0 B | 0 allocs | *Чистый процессорный пайплайн* |
| **`aoni/fast` (HTTP/1.1 TCP)** | **151 446 – 193 000 RPS** | **6.6 мкс** (6 603 ns) | **2 160 B** | **21 allocs** | **8.7x – 11.1x по RPS** |
| `net/http` (HTTP/1.1 TCP) | 17 356 RPS | 57.6 мкс (57 616 ns) | 11 230 B | 83 allocs | Базовый уровень (1.0x) |
| **`aoni/fast` (HTTP/2 TLS)** | **60 569 – 69 300 RPS** | **16.5 мкс** (16 510 ns) | **3 197 B** | **32 allocs** | **1.4x – 1.6x по RPS** |
| `net/http` (HTTP/2 TLS) | 43 385 RPS | 23.0 мкс (23 049 ns) | 8 569 B | 73 allocs | Базовый уровень (1.0x) |
| **`aoni/fast` (HTTP/3 QUIC)** | **1 091 RPS** | **916 мкс** (0.91 ms) | 130 404 B | 155 allocs | *Мультиплексированный UDP* |

### 2. Последовательная задержка (1 поток)

| Протокол | Запросов в сек (**RPS**) | Задержка (`ns/op`) | Микроархитектурные особенности |
| :--- | :---: | :---: | :--- |
| **HTTP/1.1 Keep-Alive** | **23 195 RPS** | 43.1 мкс | Однопоточный синхронный TCP roundtrip |
| **HTTP/2 TLS Multiplex** | **12 468 RPS** | 80.2 мкс | Шифрование TLS-фреймов + сжатие заголовков HPACK |
| **HTTP/3 QUIC Multiplex** | **50.5 RPS** | 19.8 мс | Ограничено таймером QUIC `MaxAckDelay` (25 мс по RFC 9000) на loopback |

### 3. Почему показатели отличаются между протоколами

* **HTTP/1.1 (Король чистой скорости в локальной сети и микросервисах)**: Записывает монолитные буферы байт напрямую в Keep-Alive TCP сокеты. Нулевой оверхед на фрейминг, задержка всего 6.6 мкс, упирается в системный потолок сетевого стека ядра ОС (~193k RPS).
* **HTTP/2 (Идеален для веб-трафика и параллельности)**: Мультиплексирует параллельные запросы в одно TLS-соединение с защитой singleflight. Оверхед HPACK и TLS-фрейминга добавляет ~10 мкс, выдавая ~70k RPS и потребляя почти в 3 раза меньше памяти, чем `net/http`.
* **HTTP/3 (Создан для мобильных сетей с потерями пакетов)**: На каждый запрос по RFC 9114 создается полноценный конечный автомат двунаправленного QUIC-стрима (`quic.Stream`, `frameSorter`, контроль окон потоков). На локальном loopback последовательная скорость ограничена таймерами ACK-задержки (~20 мс), но параллельно выдает >1000 RPS. Главное архитектурное преимущество H3 — полное отсутствие блокировок Head-of-Line при потерях UDP-пакетов и миграция соединений (Connection Migration) без обрыва сессий.

## Мост совместимости: `fast.NewStdClient`

Кричали: *«fasthttp несовместим со стандартными интерфейсами Go! Его нельзя использовать в нормальных библиотеках!»*

Можно. Для этого сделан адаптер:

```
[ Сторонний SDK / Легаси-код ]
               │
               ▼
      *http.Client / RoundTripper
               │
               ▼
     [ aoni/fast.Bridge ]  <-- Адаптер
               │
               ▼
  [ fasthttp + uTLS + H2/H3 ] --> [ Прямая запись в сокет ]
```

Код думает, что неспешно едет на стандартном `http.RoundTripper`. А под капотом молотит `aoni/fast` на миллионах RPS, и процессор внезапно перестает греть комнату.

## 🛡️ Безопасность и соответствие RFC

Движок **`aoni/fast`** объединяет скорость `fasthttp` с механизмами надежности:

1. **Управление памятью и `sync.Pool`**:
   - `BodyBytes()` возвращает безопасную копию среза (`slices.Clone`), предотвращая повреждение памяти при возврате объекта в `sync.Pool`.
   - Метод `UnsafeBodyBytes()` для сценариев с нулевым копированием.
   - Передача владения ресурсами при отмене контекста: фоновая горутина возвращает буферы в `sync.Pool` только после завершения I/O.
   - Изолированные клоны запросов при хеджировании (`executeWithHedging`).

2. **Потоковый ввод-вывод (I/O)**:
   - Потоковая передача тела запроса через `SetBodyStreamWriter`.
   - Автоматический `GetBody` с `Seek` для повторной отправки тела при 307/308 редиректах.
   - Защита от декомпрессионных бомб: распаковка выполняется строго с контролем `SizeLimit`.
   - Keep-Alive Slurping: считывание остатка ответа (до 2 KB) в `io.Discard` при закрытии или редиректе.

3. **Безопасность протоколов**:
   - Защита от HTTP Request Smuggling (RFC 9112): валидация и дедупликация заголовков `Content-Length`.
   - Лимит размера заголовков HPACK (RFC 7541, до 10 МБ).
   - Защита от флуда служебными кадрами (`PING`, `SETTINGS`).
   - Очистка заголовка `Cookie` при междоменных редиректах (RFC 6265).
   - Удаление `Referer` при переходе с HTTPS на HTTP (RFC 7231).
   - Извлечение UserInfo из URL в заголовок `Authorization: Basic`.

4. **Протоколы H1, H2, H3**:
   - HTTP/1.1 с сохранением регистра и порядка заголовков (`HeaderOrderingConn`).
   - Flow Control для HTTP/2 на базе `sync.Cond`.
   - Управление жизненным циклом HTTP/2 стримов (FSM).
   - Поддержка трейлеров HTTP/2 и HTTP/3.
   - Happy Eyeballs для HTTP/3 QUIC с откатом на H2/H1 при блокировках UDP.
   - Кэширование `Alt-Svc: h3` (RFC 7838).
   - Нормализация IDN Punycode и IPv6 Zone ID.
   - Поддержка `Expect: 100-continue`.

5. **Совместимость со стандартной библиотекой**:
   - Флаг `Response.Uncompressed`.
   - Повтор запроса при 0-Byte Write (`nothingWrittenError`) на простаивающих Keep-Alive сокетах.
   - Регистрация кастомных схем протоколов (`RegisterProtocol` / `WithProtocol`).
   - Инспекция промежуточных ответов через `httptrace.Got1xxResponse`.

## Быстрый старт

### 1. Нативный `fast.Client`

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func main() {
	ctx := context.Background()

	// Инициализация fast-клиента с TLS-отпечатками Chrome
	client := fast.NewClient(
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	resp, err := client.Request(ctx, "GET", "/users/123",
		mod.WithHeader("X-High-Load", "true"),
	)
	if err != nil {
		panic(err)
	}
	defer resp.Close() // Возвращает структуры обратно в sync.Pool

	fmt.Printf("Status: %d, Body: %s\n", resp.StatusCode(), resp.BodyBytes())
}
```

### 2. Подключение через `fast.NewStdClient`

Адаптация `aoni/fast` для библиотек и SDK, ожидающих `*http.Client`:

```go
package main

import (
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

func main() {
	fastClient := fast.NewClient(
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithProxyString("socks5://127.0.0.1:1080"),
	)

	// Адаптер для net/http.Client
	stdClient := fast.NewStdClient(fastClient)

	resp, err := stdClient.Get("https://api.target.com/data")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
}
```

## Лицензия

Распространяется под лицензией **BSD 3-Clause**. Подробности в файле [LICENSE](LICENSE).

<div align="center">
  <sub>Строгая геометрия памяти. Заберите свои процессорные такты обратно.</sub>
</div>
