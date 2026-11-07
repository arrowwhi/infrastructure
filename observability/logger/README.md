## Logger

Лёгкая обёртка над `zerolog` с поддержкой:
- Глобального логгера с инициализацией один раз
- Маскирования чувствительных данных (телефон, карта, email, пароли) на уровне `io.Writer`
- Удобных пакетных функций: `Info(ctx)`, `Warn(ctx)`, `Debug(ctx)`, `Error(ctx)`, `Fatal(ctx)`
- Автоматического извлечения полей из `context.Context`
- Гибкой конфигурации: уровень, стек‑трейсы, человеко‑читаемый вывод, маскирование
- Постоянных полей (`With()`, `WithContext()`)

### Установка

```bash
go get "github.com/arrowwhi/infrastructure/observability/logger"
```

### Конфигурация

```go
type Config struct {
    Level        string // debug, info, warn, error, fatal
    Stacktrace   bool   // добавляет стек-трейсы для Error
    PrettyOutput bool   // человеко-читаемый формат
    Masking      bool   // автоматическое маскирование данных
}
```

- **Level** — уровень логирования (`debug`, `info`, `warn`, `error`, `fatal`)
- **Stacktrace** — добавляет стек-трейс при вызове `Error()`
- **PrettyOutput** — человеко‑читаемый формат для локальной разработки
- **Masking** — автоматическое маскирование чувствительных данных

### Инициализация

```go
import "github.com/arrowwhi/infrastructure/observability/logger"

func main() {
    cfg := &logger.Config{
        Level:        "info",
        Stacktrace:   true,
        PrettyOutput: false, // true для локальной разработки
        Masking:      true,  // рекомендуется в проде
    }

    // Инициализация глобального логгера (вызывается один раз)
    // Параметры app, version, environment добавляются в каждый лог
    logger.Init(cfg, "my-service", "1.0.0", "production")

    // Готово к использованию
    logger.Info(ctx).Msg("application started")
}
```

### App, Version и Environment

Параметры `app`, `version` и `environment` добавляются в каждый лог автоматически как постоянные поля. Это позволяет легко фильтровать логи по сервису, версии и окружению в системах мониторинга.

```json
{
  "level": "info",
  "app": "my-service",
  "version": "1.0.0",
  "env": "production",
  "time": "2025-11-13T08:09:10+03:00",
  "message": "application started"
}
```

Эти поля можно оставить пустыми (передать `""`), если они не нужны:

```go
logger.Init(cfg, "", "", "")  // без app/version/env полей
```

### Базовое использование

```go
ctx := context.Background()

// Простое логирование
logger.Info(ctx).Msg("application started")
logger.Debug(ctx).Str("component", "db").Msg("connecting to database")
logger.Warn(ctx).Int("retry", 3).Msg("retrying operation")

// Логирование ошибок
if err != nil {
    logger.Error(ctx).
        Err(err).
        Str("operation", "createOrder").
        Int("userId", 123).
        Msg("operation failed")
}

// Fatal завершает приложение с os.Exit(1)
logger.Fatal(ctx).Msg("critical error occurred")
```

### Работа с контекстом

#### Автоматическое извлечение полей

```go
// Настройка извлечения полей из контекста
logger.SetContextKeys("request_id", "user_id", "trace_id")

// Добавление значений в контекст
ctx = context.WithValue(ctx, "request_id", "req-123")
ctx = context.WithValue(ctx, "user_id", "user-456")

// Поля автоматически добавятся в лог
logger.Info(ctx).Msg("processing request")
// Output: {"level":"info","request_id":"req-123","user_id":"user-456","message":"processing request"}
```

#### Кастомный экстрактор

```go
// Для более сложной логики
logger.SetContextExtractor(func(evt *zerolog.Event, ctx context.Context) *zerolog.Event {
    if reqID := ctx.Value("request_id"); reqID != nil {
        evt = evt.Str("req_id", reqID.(string))
    }
    if user := getUserFromContext(ctx); user != nil {
        evt = evt.Str("user_email", user.Email)
    }
    return evt
})
```

### Постоянные поля

#### With() - базовые постоянные поля

```go
// Создание логгера с постоянными полями
serviceLogger := logger.With().
    Str("service", "payment").
    Str("version", "1.0.0").
    Logger()

// Все логи будут содержать эти поля
serviceLogger.Info().Msg("service started")
// Output: {"level":"info","service":"payment","version":"1.0.0","message":"service started"}
```

#### WithContext() - поля из контекста

```go
// Извлечение полей из контекста при создании логгера
ctx = context.WithValue(ctx, "request_id", "req-123")
ctx = context.WithValue(ctx, "user_id", "user-456")

requestLogger := logger.WithContext(ctx, "request_id", "user_id").Logger()

// Все логи от этого логгера будут содержать поля из контекста
requestLogger.Info().Msg("request started")
requestLogger.Info().Msg("request completed")
```

### Маскирование данных

При `Masking: true` автоматически маскируются:

```go
logger.Info(ctx).
    Str("phone", "+7 (999) 123-45-67").      // -> "+7 (9**) ***-**-67"
    Str("card", "4111111111111111").         // -> "411111******1111"
    Str("email", "user@example.com").        // -> "us**@example.com"
    Str("password", "secret123").            // -> "*****"
    Str("full_name", "Иван Иванов").         // -> "И*** И*****"
    Msg("user data logged")
```

Маскирование работает на уровне `io.Writer` и применяется ко всем логам автоматически.

### Типы полей

Поддерживаемые типы для добавления полей:

```go
logger.Info(ctx).
    Str("string", "value").
    Bool("active", true).
    Int("count", 42).
    Int64("bignum", 123456789).
    Uint64("unsigned", 999).
    Float64("price", 99.99).
    Bytes("data", []byte("binary")).
    Time("created", time.Now()).
    Dur("elapsed", time.Second).
    Err(err).
    Any("complex", customStruct).
    Msg("all types example")
```

### Продвинутые примеры

#### HTTP middleware с контекстными полями

```go
func LoggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        requestID := uuid.New().String()
        ctx := context.WithValue(r.Context(), "request_id", requestID)
        ctx = context.WithValue(ctx, "method", r.Method)
        ctx = context.WithValue(ctx, "path", r.URL.Path)

        logger.Info(ctx).Msg("request started")
        
        next.ServeHTTP(w, r.WithContext(ctx))
        
        logger.Info(ctx).Msg("request completed")
    })
}
```

#### Логирование с метриками производительности

```go
func ProcessOrder(ctx context.Context, order Order) error {
    start := time.Now()
    
    logger.Info(ctx).
        Str("order_id", order.ID).
        Msg("processing order")

    // ... business logic ...

    logger.Info(ctx).
        Str("order_id", order.ID).
        Dur("duration", time.Since(start)).
        Msg("order processed successfully")

    return nil
}
```

#### Структурированное логирование ошибок

```go
func HandleError(ctx context.Context, err error, operation string) {
    logger.Error(ctx).
        Err(err).
        Str("operation", operation).
        Str("error_type", fmt.Sprintf("%T", err)).
        Stack().  // добавит стек при Stacktrace: true
        Msg("operation failed")
}
```

### JSON vs Pretty Output

#### JSON (production)

```go
cfg := &logger.Config{
    Level:        "info",
    PrettyOutput: false,
}
// Output: {"level":"info","time":"2024-01-15T12:34:56Z","message":"user logged in","user_id":"123"}
```

#### Pretty (development)

```go
cfg := &logger.Config{
    Level:        "debug",
    PrettyOutput: true,
}
// Output: 15.01.2024 12:34:56 INFO | user logged in | user_id=123
```

### Рекомендации

- **Production**: `PrettyOutput: false`, `Masking: true`, `Level: "info"`
- **Development**: `PrettyOutput: true`, `Masking: false`, `Level: "debug"`
- **Staging**: `PrettyOutput: false`, `Masking: true`, `Level: "debug"`

### Best Practices

1. **Используйте контекст для трейсинга**: передавайте `context.Context` через все слои приложения
2. **Настройте SetContextKeys() один раз**: в `main()` или `init()`
3. **Избегайте логирования в циклах**: агрегируйте данные и логируйте итоги
4. **Не логируйте пароли напрямую**: даже с маскированием, лучше вообще не логировать
5. **Используйте уровни логирования правильно**:
   - `Debug`: детальная отладочная информация
   - `Info`: важные события в нормальном потоке
   - `Warn`: потенциальные проблемы, не критичные
   - `Error`: ошибки, требующие внимания
   - `Fatal`: критические ошибки, приводящие к остановке

### Примечания

- `Init()` использует `sync.Once` — безопасно вызывать многократно, но инициализируется только один раз
- `Fatal().Msg()` логирует и завершает процесс через `os.Exit(1)`
- Маскирование выполняется на уровне `io.Writer` до записи в output
- Все функции логирования потокобезопасны
- API следует паттерну method chaining из zerolog

### Производительность

Логгер основан на `zerolog` — одной из самых быстрых библиотек логирования для Go:
- Нулевые аллокации для базовых типов
- Отложенная сериализация JSON
- Эффективная работа с memory pool
- Маскирование работает in-place где возможно
