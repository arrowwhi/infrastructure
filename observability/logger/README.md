# observability/logger

Глобальный структурированный логгер поверх [zerolog](https://github.com/rs/zerolog).
Пакетные функции `Info(ctx)`, `Warn(ctx)`, `Error(ctx)`… сами добавляют в запись
`trace_id`/`span_id` активного спана и поля из контекста, а маскирование на уровне
`io.Writer` скрывает телефоны, карты, e-mail и пароли до того, как строка попадёт в вывод.
Остальные пакеты библиотеки пишут логи через него.

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/observability/logger"
```

## Пример

```go
package main

import (
	"context"
	"errors"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

func main() {
	ctx := context.Background()

	logger.Init(&logger.Config{
		Level:   "info",
		Masking: true,
	}, "my-service", "1.0.0", "production")

	logger.Info(ctx).Str("user_id", "42").Msg("user logged in")
	logger.Error(ctx).Err(errors.New("boom")).Str("op", "createOrder").Msg("operation failed")
}
```

```json
{"level":"info","app":"my-service","version":"1.0.0","env":"production","user_id":"42","time":"...","message":"user logged in"}
```

Если `ctx` несёт активный спан (например, внутри HTTP-обработчика с трейсингом), в запись
автоматически попадают `"trace_id"` и `"span_id"` — по ним Grafana/Loki связывает лог с трейсом.

### Поля из контекста

```go
type ctxKey string

logger.SetContextKeys("request_id")                 // ключи-строки из ctx.Value
ctx = context.WithValue(ctx, "request_id", "req-1") //nolint:staticcheck // ключ должен быть строкой

// Для типизированных ключей — свой экстрактор:
logger.SetContextExtractor(func(e *zerolog.Event, ctx context.Context) *zerolog.Event {
	if v, ok := ctx.Value(ctxKey("tenant")).(string); ok {
		e = e.Str("tenant", v)
	}
	return e
})
```

### Логгер с постоянными полями и «сырые» zerolog-логгеры

```go
l := logger.With().Str("component", "grpc-client").Logger() // zerolog.Logger
l.Info().Msg("started")                                      // trace_id сюда сам не попадёт

traced := l.Hook(logger.TraceHook{})
traced.Info().Ctx(ctx).Msg("call") // TraceHook берёт trace_id/span_id из Event.Ctx(ctx)
```

## Конфигурация

Префикс переменных задаёт тот, кто встраивает `Config`; в `telegram/app` это `LOG_`.

| Переменная (`telegram/app`) | По умолчанию | Смысл |
|---|---|---|
| `LOG_LEVEL` | `info` | `trace`, `debug`, `info`, `warn`, `error`, `fatal` (неизвестное значение → `info`) |
| `LOG_PRETTYOUTPUT` | `false` | человекочитаемый цветной вывод для локальной разработки |
| `LOG_MASKING` | `true` | маскирование чувствительных полей |
| `LOG_STACKTRACE` | `false` | зарезервировано, сейчас ни на что не влияет |

## Что маскируется

При `Masking: true` вывод проходит через [`utils/masking`](../../utils/masking/README.md):

| Поле (по имени ключа) или значение | Пример |
|---|---|
| ключ `*phone*` | `+7 (999) 123-45-67` → `+* (***) ***-45-67` |
| ключ `card`, `cardNumber`; число из 16+ цифр в любой строке | `4111111111111111` → `41**********1111` |
| ключ `full_name`, `fullname` | `Иван Иванов` → `**** ******` |
| ключ `email`, `e-mail`, `e_mail`; строка-e-mail целиком | `john.doe@example.com` → `********@example.com` |
| ключ `*password*`, `*token*`, `*secret*` | → `*****` |

## Метрики, трейсы, логи

Метрик и трейсов не пишет. Поля каждой записи: `level`, `time`, `message`, `app`, `version`,
`env` (пустые параметры `Init` не добавляются), `trace_id`/`span_id` при активном спане.

## Связь с другими пакетами

- `telegram/app` вызывает `logger.Init` сам из `app.Config.Logger`.
- `http/server`, `grpc/server`, `telegram/bot`, `connections/postgres` пишут через пакетные
  функции — их логи получают `trace_id` автоматически.
- `grpc/client` принимает `*zerolog.Logger` и сам навешивает `TraceHook`; передайте
  `l := logger.With().Logger(); client.NewClient(&cfg, &l)`.

## Подводные камни

- `Init` срабатывает один раз (`sync.Once`): повторные вызовы игнорируются. До `Init`
  используется временный логгер уровня `debug` без маскирования.
- `trace_id` появляется только если передать в `Info(ctx)` контекст со спаном; `context.Background()`
  его не несёт.
- `SetContextKeys` ищет значения по **строковым** ключам `ctx.Value("key")`; для типизированных
  ключей используйте `SetContextExtractor`.
- `Fatal(ctx).Msg(...)` завершает процесс через `os.Exit(1)` — отложенные функции не выполнятся.
- Маскирование стоит CPU на каждой записи; не выключайте его в проде ради скорости — лучше не
  логировать большие тела.
- `logger.New` возвращает `*Logger` без экспортированных методов; для собственного логгера
  используйте `logger.With()...Logger()`.
