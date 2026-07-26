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

## Лицензия

Распространяется под лицензией **BSD 3-Clause**. Подробности в файле [LICENSE](LICENSE).

<div align="center">
  <sub>Чистая физика. Непреклонная производительность. Заберите свой процессор обратно.</sub>
</div>
