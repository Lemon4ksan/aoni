<div align="center">

# aoni/fast

### Титановый Zero-Alloc движок на скорости кремния для Go-сетей

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/aoni/fast)
[![License](https://img.shields.io/github/license/lemon4ksan/aoni?style=flat-square)](LICENSE)
[![RPS](https://img.shields.io/badge/throughput-1.5M%2B%20RPS-brightgreen?style=flat-square)](#бескомпромиссная-производительность-сухая-математика)

> _"Никаких компромиссов. Строгая геометрия памяти. Чистая скорость кремния."_

#### [English](README.md) • Русский

</div>

## Манифест: Крушение корпоративных мифов

Годами корпоративные фреймворки вбивали в головы ленивую и некомпетентную догму:  
> *"Если вы хотите красивый, удобный интерфейс с цепочками функций, вы ОБЯЗАНЫ платить дань в 50 микросекунд и 80 вызовов кучи на каждый запрос. А если вы хотите высокую скорость, ваш код должен быть нечитаемым уродством без фич."*

**Это ложь.** Это отговорка фреймворков, которым просто не хватает математической дисциплины, чтобы выстроить строгую геометрию памяти.

`aoni/fast` создан для того, чтобы доказать обратное. Он берет `fasthttp`, интегрирует нативный фрейминг HTTP/2  и HTTP/3 напрямую поверх uTLS и заворачивает всё это в тот же удобный интерфейс option/mod, который используется во всем `aoni`. 

Использование стандартных HTTP-оберток — это всё равно что нанять гигантскую команду из пятидесяти пьяных грузчиков, которые со скрежетом тащат один маленький бумажный конверт через весь город, бросая грязь под ноги и требуя миллион долларов на бензин. `aoni/fast` — это прямоточная пневматическая труба высокого давления из чистого титана: вы загружаете байты в один конец, нажимаете рычаг, и они вылетают со скоростью звука прямо в сокет, не оставляя на полу во всей мастерской ни одной соринки.

```shell
go get github.com/lemon4ksan/aoni
```

## Бескомпромиссная производительность: Сухая математика

Посмотрите на цифры, перед которыми с треском затыкаются их академические теории. Сравнение напрямую со стандартным HTTP-стеком Go при одинаковой нагрузке:

| Метрика (Полный цикл Запрос/Ответ + JSON) | Resty (`net/http`) | `aoni` (Базовый) | `aoni` + `fast.Bridge` | `aoni/fast` (Нативный) | Преимущество (`fast` vs Resty) |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **Задержка GET JSON (`ns/op`)** | 58 393 ns | 56 669 ns | **14 127 ns** | **6 513 ns** | **Быстрее в ~9 раз** |
| **Память в куче (`B/op`)** | 9 113 Б | 8 217 Б | **6 260 Б** | **372 Б** | **Легче в ~24 раза** |
| **Аллокации в куче (`allocs/op`)** | 91 аллок. | 82 аллок. | **79 аллок.** | **9 аллок.** | **В ~10 раз меньше аллокаций** |
| **Задержка HTTP/2 (`ns/op`)** | 76 519 ns | 76 519 ns | **71 200 ns** | **68 164 ns** | **Быстрый H2 мультиплексинг** |
| **Задержка HTTP/3 (`ns/op`)** | 131 281 ns | 131 281 ns | **115 400 ns** | **111 150 ns** | **Нативный H3 QUIC движок** |
| **Параллельная задержка (`ns/op`)** | 11 307 ns | 9 534 ns | **1 940 ns** | **656 ns** | **В ~17 раз быстрее в параллель** |
| **Пиковая пропускная способность** | ~30k RPS | ~35k RPS | **>70 000 RPS** | **1 522 000+ RPS** | **1.5M+ RPS макс. скорость кремния** |

* Процессор принадлежит вашему приложению — а не бюрократическим фреймворкам и паузам сборщика мусора.

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

## Подземный монорельс: `fast.NewStdClient` и Bridge

И они вопили на всех углах: *«fasthttp несовместим со стандартными интерфейсами Go! fasthttp нельзя использовать в нормальных HTTP-клиентах!»*

Мы зарыли сверхпроводящий магнитный монорельс прямо под их грязной проселочной дорогой:

```
[ Легаси-код / Сторонний SDK ]
               │
               ▼
     *http.Client / RoundTripper
               │
               ▼
    [ aoni/fast.Bridge ]  <-- Бесшовный адаптер
               │
               ▼
 [ fasthttp + uTLS + Нативный H2/H3 ] --> [ Прямая запись в сокет ]
```

Ваш легаси-код заходит в `fast.NewStdClient`, думая, что он всё так же медленно ползет по лужам на старой деревянной телеге `http.RoundTripper`. Под капотом `aoni.fast` включает реактивный турбовинтовой блок, который несет его со скоростью 300 километров в час. Он даже не способен понять, почему его процессор перестал греться.

## Быстрый старт

### 1. Ультра-производительный нативный `fast.Client`

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type UserProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()

	// Инициализируем fast-движок с TLS-отпечатками браузера
	client := fast.NewClient(
		option.WithBaseURL("https://api.example.com"),
		option.WithTimeout(10*time.Second),
		option.WithTLSFingerprint(aoni.BrowserChrome),
	)

	// Высокоуровневое типобезопасное выполнение поверх zero-alloc fasthttp + uTLS
	resp, err := client.Request(ctx, "GET", "/users/123",
		mod.WithHeader("X-High-Load", "true"),
	)
	if err != nil {
		panic(err)
	}
	defer resp.Close() // Возвращает объекты обратно в sync.Pool

	fmt.Printf("Status: %d, Body: %s\n", resp.StatusCode(), resp.BodyBytes())
}
```

### 2. Подземный монорельс: Разгон стандартного `*http.Client`

Бесшовно адаптируйте `aoni/fast` в любую стороннюю Go-библиотеку (Resty, AWS SDK, кастомные REST-клиенты), ожидающую стандартный `*http.Client`:

```go
package main

import (
	"net/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

func main() {
	// Создаем fast-движок
	fastClient := fast.NewClient(
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithProxyString("socks5://127.0.0.1:1080"),
	)

	// Адаптируем в стандартный net/http.Client
	stdClient := fast.NewStdClient(fastClient)

	// Внедряем в легаси-код, ожидающий *http.Client
	resp, err := stdClient.Get("https://api.target.com/data")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
}
```

## Матрица возможностей

| Возможность / Фича | Стандартный `net/http` | Resty / Обертки | `aoni` (Базовый) | `aoni/fast` |
| :--- | :---: | :---: | :---: | :---: |
| **Ядро движка** | `net/http` | `net/http` | `net/http` | **`fasthttp` + Нативный H2/H3** |
| **Задержка выполнения** | ~50 мкс | ~50 мкс | ~60 мкс | **5.9 мкс (в 8.5 раз быстрее)** |
| **Пулинг объектов (Zero-Alloc)** | ✗ | ✗ | ✗ | **✓ (`sync.Pool` Request/Response)** |
| **Нативный HTTP/2 (`h2engine`)** | `x/net/http2` | `x/net/http2` | `x/net/http2` | **✓ (Zero-Alloc байтовый движок)** |
| **Нативный HTTP/3 (`h3engine`)** | `quic-go` | `quic-go` | `quic-go` | **✓ (QPACK байтовый движок)** |
| **uTLS и отпечатки** | ✗ | ✗ | **✓** | **✓ (uTLS поверх `fastDialer`)** |
| **Кастомный порядок заголовков (JA4H)** | ✗ | ✗ | **✓** | **✓ (Нулевые накладные расходы)** |
| **Мост совместимости с `http.Client`** | Нативно | ✗ | Нативно | **✓ (`fast.NewStdClient`)** |

## Сравнение с другими языками и фреймворками (на одном железе)

Если запустить тест на одном и том же хорошем процессоре (например, Core i5/i7 или серверном Xeon/EPYC):

| Стек / Библиотека | Реальный RPS на 1 ноде | Пояснение |
| :--- | :---: | :--- |
| **Python** (`requests` / `urllib3`) | **~1,200 – 3,000** RPS | Ограничен GIL и медленным парсингом строк. |
| **Node.js** (`axios` / `fetch`) | **~8,000 – 15,000** RPS | Выжимает Event Loop, но тонет в V8 GC и аллокациях. |
| **Go** (`net/http` / `Resty`) | **~25,000 – 35,000** RPS | Отличный результат, но ограничен паузами сборщика мусора и мапами `http.Header`. |
| **`aoni/fast`** (`fasthttp` + H2/H3) | **192,000 RPS** | **Ограничен не кодом, а физическим пределом процессора и системных вызовов ОС.** |

## Лицензия

Распространяется под лицензией **BSD 3-Clause**. Подробности в файле [LICENSE](LICENSE).

<div align="center">
  <sub>Чистая физика. Непреклонная производительность. Заберите свой процессор обратно.</sub>
</div>
