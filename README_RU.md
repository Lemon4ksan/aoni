<div align="center">

# aoni

### Высокопроизводительный безаллокационный сетевой движок для Go HTTP, Protobuf и сетей реального времени

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)

> _"Нуль компромиссов. Дисциплина нулевых аллокаций. Несокрушимая сетевая стойкость."_

#### [English](README.md) • Русский

</div>

## Почему Aoni?

При разработке современных приложений на Go перед инженером часто встает нежелательный компромисс: использовать примитивную библиотеку-обертку ради минимальной задержки или писать тысячи строк шаблонного кода для решения реальных сетевых задач, таких как изоляция прокси, gRPC-Web, TLS-фингерпринтинг и обход WAF-защит.

`aoni` устраняет этот компромисс. Он спроектирован на основе профилирования и дисциплины нулевых аллокаций. Библиотека превосходит стандартные HTTP-обертки как по объему потребляемой памяти, так и по скорости исполнения, предоставляя при этом полный спектр технологий браузерной маскировки, uTLS, JA4 и gRPC-Web.

```shell
go get github.com/lemon4ksan/aoni
```

## Бескомпромиссная производительность: Доказано `pprof`

`aoni` работает на самом физическом пределе эффективности рантайма Go. Прямое сравнение с популярными HTTP-библиотеками на идентичных нагрузках показывает:

| Метрика | Стандартная обертка (Resty) | `aoni` (Стандартный) | `aoni` + `fast.Bridge` | `aoni/fast` (Нативный) | Преимущество `fast` |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **Задержка GET JSON (`ns/op`)** | 58 393 ns | 56 669 ns | 14 127 ns | 6 513 ns | **В ~9 раз быстрее** |
| **Потребление памяти (`B/op`)** | 9 113 B | 8 217 B | 6 260 B | 372 B | **В ~24 раза легче** |
| **Число аллокаций (`allocs/op`)** | 91 allocs | 82 allocs | 79 allocs | 9 allocs | **В ~10 раз меньше аллокаций** |
| **Задержка HTTP/2 (`ns/op`)** | 76 519 ns | 76 519 ns | 71 200 ns | 68 164 ns | **Быстрый H2 мультиплексинг** |
| **Задержка HTTP/3 (`ns/op`)** | 131 281 ns | 131 281 ns | 115 400 ns | 111 150 ns | **Нативный H3 QUIC движок** |
| **Параллельная задержка (`ns/op`)** | 11 307 ns | 9 534 ns | 1 940 ns | 656 ns | **В ~17 раз быстрее в параллель** |
| **Пиковая пропускная способность** | ~30k RPS | ~35k RPS | >70 000 RPS | 1 522 000+ RPS | **Пиковая скорость кремния** |
| **Накладные расходы билдера (`.R()`)** | 32 B / 2 allocs | 32 B / 2 allocs | 32 B / 2 allocs | 32 B / 2 allocs | **Нулевой оверхед пула** |

Независимо от того, вызываете ли вы стандартные микросервисные REST API или парсите миллионы страниц за защитой Cloudflare/Akamai, `aoni` обеспечивает максимальную скорость без потерь.

## Единая эргономика

Независимо от того, выберете ли вы стандартный `aoni` или `aoni/fast`, вы будете управлять автомобилем с одинаково удобным рулевым колесом:

```
               ┌──► aoni.Client (100% совместимость с net/http и middleware)
option / mod ──┼
               └──► fast.Client (1.5M+ RPS multi-core, 656ns параллельная задержка, zero-alloc fasthttp + H2/H3)
```

* **Нужна 100% совместимость со стандартной библиотекой и сложное промежуточное ПО?** Используйте `aoni`.
* **Нужна абсолютная, чистая пропускная способность кремния и геометрия нулевого выделения памяти?** Используйте [`aoni/fast`](fast).

## 🛡️ Полное соответствие RFC, безопасность и почему нет причин использовать `net/http` вместо `aoni/fast`

Движок **`aoni/fast`** объединяет скорость и нулевые аллокации `fasthttp` с защитой безопасности и стандартами академического уровня `net/http`:

1. **Управление памятью, `sync.Pool` и предотвращение Use-After-Free / Data Race**:
   - Безопасная копия тела ответа (`slices.Clone` в `BodyBytes()`), предотвращающая повреждение памяти при возврате объекта в `sync.Pool`.
   - Явный Zero-Copy метод `UnsafeBodyBytes()` для прецизионных высоконагруженных сценариев.
   - Передача владения памятью (Ownership Transfer) при отмене контекста: фоновая горутина возвращает ресурсы в `sync.Pool` только после завершения I/O, исключая Data Race.
   - Защита от Data Race при хеджировании (`executeWithHedging`) с созданием изолированных клонов запросов.

2. **Потоковый ввод-вывод (I/O) и защита от OOM**:
   - Потоковая передача тел запросов (Streaming Body) через `SetBodyStreamWriter` без вычитывания гигабайтных файлов в память.
   - Автоматический `GetBody` с rewind (`Seek`) для повторной отправки потоковых тел при 307/308 редиректах.
   - Защита от Zip-бомб: декомпрессия (`gzip`/`brotli`/`zstd`) выполняется строго перед `SizeLimit` лимитом.
   - Keep-Alive Slurping: считывание невычитанного остатка (до 2 KB) в `io.Discard` при закрытии/редиректе для сохранения сокета.

3. **Безопасность протоколов (RFC Standards & Anti-Exploit)**:
   - HTTP Request Smuggling Protection (RFC 9112): дедупликация и отказ от обработки при конфликтах `Content-Length`.
   - HPACK Header Flood Limit (RFC 7541): жесткий лимит объема заголовков (10 МБ).
   - Control Frame Flood Protection (Anti-DoS): расторжение соединения при спаме служебными кадрами (`PING`, `SETTINGS` >1000 подряд).
   - Subdomain-Aware Cookie Scrubbing (RFC 6265): зачистка `Cookie` при Cross-Domain редиректах.
   - HTTPS ➔ HTTP Referer Strip (RFC 7231): автоудаление `Referer` при даунгрейде схемы.
   - Автоизвлечение UserInfo из URL в `Authorization: Basic`.

4. **Протоколы H1, H2, H3 и сетевой стек**:
   - HTTP/1.1 Anti-DPI (`HeaderOrderingConn`) с сохранением регистра и порядка заголовков.
   - H2 Flow Control без Spin-Wait на условных переменных (`sync.Cond`).
   - H2 Stream Lifecycle FSM (`streamIdle`, `streamOpen`, `streamHalfClosed`, `streamClosed`).
   - Поддержка HTTP/2 & HTTP/3 Trailers и вычитка QPACK Encoder Stream.
   - HTTP/3 QUIC Happy Eyeballs с авто-откатом на H2/H1 при блокировках UDP 443.
   - Кэширование `Alt-Svc: h3` (RFC 7838).
   - IDN Punycode & IPv6 Zone ID Stripping (`[fe80::1%eth0]` ➔ `[fe80::1]`).
   - Domain Fronting изоляция SNI (RFC 6066).
   - Поддержка паузы `Expect: 100-continue`.

5. **Академические механизмы stdlib `net/http`**:
   - Флаг `Response.Uncompressed`.
   - `nothingWrittenError` (0-Byte Write Retry) для безопасного повтора на заснувших Keep-Alive сокетах.
   - Реестр кастомных схем (`RegisterProtocol` / `WithProtocol`).
   - Инспекция промежуточных ответов через `httptrace.Got1xxResponse`.

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
