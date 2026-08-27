<div align="center">

# aoni/fast

### Zero-Alloc сетевой движок для Go

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/fast)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
[![RPS](https://img.shields.io/badge/throughput-2.00M%2B%20RPS-brightgreen?style=flat-square)](#матрица-возможностей)

> _"Строгая геометрия памяти. Чистая скорость железа."_

#### [English](README.md) • Русский

</div>

## Манифест: Против раздутого софта

Годами фреймворки внушали ленивую догму:  
> *"Хочешь удобный интерфейс с цепочками методов — плати 50 микросекунд задержки и 80 аллокаций в куче на каждый чих. А если нужна скорость — пиши нечитаемый спагетти-код на голых указателях."*

Это ерунда и оправдание лени.

`aoni/fast` берет `fasthttp`, прикручивает нативный фрейминг HTTP/2 и HTTP/3 прямо поверх uTLS и упаковывает всё это в единый лаконичный интерфейс option/mod из `aoni`. 

Использовать стандартные жирные HTTP-обертки — это как нанять толпу из пятидесяти пьяных грузчиков, которые со скрежетом тащат один бумажный конверт через весь город, месят грязь и требуют цистерну бензина. `aoni/fast` — это прямоточная пневматическая труба: загружаешь байты в сокет, дергаешь рычаг, и они вылетают на провод без единой лишней аллокации на полу.

```shell
go get github.com/lemon4ksan/aoni
```

## Матрица возможностей

| Возможность / Фича | Стандартный `net/http` | Resty / Обертки | `aoni` (Базовый) | `aoni/fast` |
| :--- | :---: | :---: | :---: | :---: |
| **Ядро движка** | `net/http` | `net/http` | `net/http` | **`fasthttp` + Нативный H2/H3** |
| **Задержка выполнения** | ~50 мкс | ~50 мкс | ~56 мкс | **5.9 мкс** |
| **Пулинг объектов (Zero-Alloc)** | ✗ | ✗ | ✗ | **✓ (`sync.Pool` Request/Response)** |
| **Нативный HTTP/2 (`h2engine`)** | `x/net/http2` | `x/net/http2` | `x/net/http2` | **✓ (Zero-Alloc байтовый движок)** |
| **Нативный HTTP/3 (`h3engine`)** | `quic-go` | `quic-go` | `quic-go` | **✓ (QPACK байтовый движок)** |
| **uTLS и отпечатки** | ✗ | ✗ | **✓** | **✓ (uTLS поверх `fastDialer`)** |
| **Кастомный порядок заголовков (JA4H)** | ✗ | ✗ | **✓** | **✓** |
| **Мост совместимости с `http.Client`** | Нативно | ✗ | Нативно | **✓ (`fast.NewStdClient`)** |

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
