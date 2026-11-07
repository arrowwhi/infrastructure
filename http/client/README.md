# HTTP клиент

### Возможности

**1. Полный набор HTTP-методов**

Методы верхнего уровня: `Get(path)`, `Post(path)`, `Put(path)`, `Delete(path)`, `Patch(path)`, а также универсальный `R(method, path)`.

**2. Fluent API**

Цепочка методов для настройки запроса: `SetQueryParam()`, `SetHeader()`, `SetBody()`.

**3. Механизм ретраев**

Настройка максимального количества попыток, базовой задержки с экспоненциальным ростом и джиттером. Автоматический ретрай при сетевых ошибках и статусах 429, 500, 502, 503, 504.

**4. Управление заголовками и телом**

Методы `SetHeader()` и `SetBody()` для настройки запроса. Автоматическая сериализация JSON/XML.

**5. Передача контекста**

Все методы принимают `context.Context` для управления отменой запроса и таймаутами.

**6. Обработка ответов и ошибок**

Типизированные ошибки `HTTPError` с HTTP-кодом, URL и телом ответа. Автоматическая десериализация ответа в структуру.

**7. Логгирование**

Интерфейс `Logger` с возможностью подключения собственного логгера. Дефолтный noop-логгер.

**8. TLS настройки**

Настройка корневых сертификатов (Custom CA), клиентских сертификатов и возможность отключения проверки TLS.

**9. Настройка пула соединений**

Конфигурация `MaxIdleConns` и `IdleConnTimeout`.

**10. Управление таймаутами**

Детальная настройка таймаутов: `DialTimeout`, `TLSHandshakeTimeout`, `ResponseHeaderTimeout`.

---

### Работа с BaseURL и абсолютными URL

- **BaseURL**: при создании клиента можно задать базовый URL в `Config.BaseURL`. Все относительные пути, переданные в методы `Get`, `Post`, `Put`, `Delete`, `Patch` и `R`, будут конкатенироваться с этим базовым URL.
- **Полный URL в запросе**: если в параметр `path` передать **полный URL** (со схемой и хостом, например `https://api.example.com/v1/users`), то:
  - этот URL будет использован **как есть**;
  - значение `BaseURL` в конфигурации **игнорируется** для такого запроса.

Примеры:

```go
// Использует BaseURL: https://jsonplaceholder.typicode.com/posts/1
client.Get(ctx, "/posts/1").Execute(&post)

// Игнорирует BaseURL и ходит прямо на указанный хост:
client.Get(ctx, "https://api.other-service.com/posts/1").Execute(&post)
```

---

### Сериализация и десериализация

По умолчанию клиент умеет:

- **Сериализовать** тело запроса для типов:
  - `Content-Type: application/json` → `encoding/json.Marshal`;
  - `Content-Type: application/xml` → `encoding/xml.Marshal`.
- **Десериализовать** ответ для типов:
  - `Content-Type: application/json` → `encoding/json.Unmarshal`;
  - `Content-Type: application/xml` → `encoding/xml.Unmarshal`.

Если `Content-Type` не поддерживается и `target` в `Execute` не является `*[]byte`, будет возвращена ошибка `ErrNoDeserializerFound`.

#### Добавление кастомных сериализаторов

Можно добавить собственные сериализаторы для нужных `Content-Type` через опции `WithSerializer` и `WithDeserializer`:

```go
client, err := httpclient.NewClient(cfg,
	httpclient.WithSerializer("application/x-msgpack", func(v interface{}) ([]byte, error) {
		// пример: msgpack.Marshal(v)
		return msgpack.Marshal(v)
	}),
	httpclient.WithDeserializer("application/x-msgpack", func(data []byte, v interface{}) error {
		// пример: msgpack.Unmarshal(data, v)
		return msgpack.Unmarshal(data, v)
	}),
)
if err != nil {
	panic(err)
}

// Дальше достаточно указать Content-Type и тело:
var res MyResponse
_, err = client.Post(ctx, "/api").
	SetHeader("Content-Type", "application/x-msgpack").
	SetBody(MyRequest{/* ... */}).
	Execute(&res)
```

---

## Пример использования

```go
package main

import (
	"context"
	"fmt"
	"time"

	httpclient "github.com/arrowwhi/infrastructure/http/client"
)

type Post struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	UserID int    `json:"userId"`
}

func main() {
	ctx := context.Background()

	cfg := httpclient.Config{
		BaseURL: "https://jsonplaceholder.typicode.com",
		Retry: &httpclient.RetryConfig{
			MaxRetries:      3,
			BackoffTime:     100 * time.Millisecond,
			BackoffFraction: 0.3,
		},
	}

	client, err := httpclient.NewClient(cfg,
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("User-Agent", "go-http-client/1.0"),
	)
	if err != nil {
		panic(err)
	}

	// GET запрос
	var post Post
	resp, err := client.Get(ctx, "/posts/1").
		SetQueryParam("_limit", 1).
		Execute(&post)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Status: %d, Post: %+v\n", resp.StatusCode, post)

	// POST запрос
	postData := Post{
		Title:  "foo",
		Body:   "bar",
		UserID: 1,
	}

	var createdPost Post
	resp, err = client.Post(ctx, "/posts").
		SetHeader("Content-Type", "application/json").
		SetBody(postData).
		Execute(&createdPost)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Status: %d, Created: %+v\n", resp.StatusCode, createdPost)

	// Обработка ошибок
	resp, err = client.Get(ctx, "/posts/99999").Execute(&post)
	if err != nil {
		if httpErr, ok := httpclient.AsHTTPError(err); ok {
			fmt.Printf("HTTP Error: %d - %s\n", httpErr.StatusCode, string(httpErr.ResponseBody))
		}
	}
}
```

---

## TODO

- Добавить тест на mTLS
- Добавить метрики
- Добавить трейсы
- Сделать логгирование оберткой функции Execute
