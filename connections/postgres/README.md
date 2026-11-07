# connections/postgres

Коннектор к PostgreSQL поверх [pgx v5](https://github.com/jackc/pgx): пул соединений,
трейсинг и метрики OpenTelemetry, транзакции через контекст, безопасные повторы,
разделение чтения и записи и набор хелперов для нестандартных задач.

```go
import "github.com/arrowwhi/infrastructure/connections/postgres"
```

## Быстрый старт

```go
cfg := postgres.DefaultConfig()
cfg.Host = "db.internal"
cfg.User = "svc"
cfg.Password = os.Getenv("DB_PASSWORD")
cfg.Database = "orders"

pool, err := postgres.New(ctx, cfg)
if err != nil {
    return err
}
defer pool.Close()
```

`Config` размечен тегами `env` и `yaml`, поэтому обычно он приезжает из общего конфига
сервиса, а не собирается руками.

## Инструментация

**По умолчанию выключена.** Когда `TracingEnabled` и `MetricsEnabled` равны `false`,
трейсер не создаётся вовсе — `ConnConfig.Tracer` остаётся `nil`, и pgx не тратит
ни такта на его вызовы. Накладные расходы строго нулевые, а не «небольшие».

Включается без пересборки:

```bash
DB_TRACING_ENABLED=true
DB_METRICS_ENABLED=true
DB_SLOW_QUERY_LOG=true
DB_SLOW_QUERY_THRESHOLD=200ms
```

Либо программно, если нужно передать провайдеры или атрибуты:

```go
pool, err := postgres.New(ctx, cfg,
    postgres.WithTracing(),
    postgres.WithMetrics(),
    postgres.WithSlowQueryLog(200*time.Millisecond),
    postgres.WithMeterProvider(mp),
    postgres.WithAttributes(attribute.String("shard", "eu-1")),
)
```

### Имена запросов

Метрики размечаются меткой `db.query.name`. Имя задаётся через контекст:

```go
ctx = postgres.WithQueryName(ctx, "user.get_by_id")
user, err := postgres.Get[User](ctx, pool, `SELECT * FROM users WHERE id = $1`, id)
```

Без имени запрос попадает в метрику как `query_name="unknown"`, а в лог один раз
падает предупреждение — по одному на уникальный текст SQL, а не на каждый вызов.

Разбивать метрики по тексту SQL нельзя: кардинальность взорвётся. Поэтому имя задаётся
явно и должно быть из ограниченного набора значений — `user.get_by_id`, а не идентификатор
конкретной сущности.

### Что попадает в трейсы и логи

Текст SQL — да. **Аргументы запроса — никогда.** Единая политика важнее удобства отладки:
параметры содержат персональные данные, и один незакрытый случай сводит на нет всё маскирование.

Трейсинг построен на [otelpgx](https://github.com/exaring/otelpgx), но он полностью скрыт
внутри пакета: сервисы его не импортируют и настраивают всё через `Config` и `Option`.

Метрики: `db.client.operation.duration` (гистограмма), `db.client.operation.errors`
(счётчик с меткой SQLSTATE), `db.client.batch.size`, плюс статистика пула.

## Транзакции

`Pool.Query` сам достаёт транзакцию из контекста, если она там есть. Один и тот же
репозиторий работает и внутри транзакции, и снаружи:

```go
func (r *UserRepo) Get(ctx context.Context, id int64) (User, error) {
    return postgres.Get[User](ctx, r.db, `SELECT * FROM users WHERE id = $1`, id)
}

// без транзакции
user, err := repo.Get(ctx, 42)

// в транзакции — тот же вызов, ноль правок в репозитории
err := pool.InTx(ctx, func(ctx context.Context) error {
    user, err := repo.Get(ctx, 42)
    if err != nil {
        return err
    }

    return repo.Update(ctx, user)
})
```

`InTx` сам делает commit при успехе и rollback при ошибке или панике.

**Вложенный `InTx` открывает `SAVEPOINT`**, а не вторую транзакцию: ошибка внутреннего
блока откатывает только его часть, внешний продолжает работу и коммитится как обычно.
Открытие второй независимой транзакции привело бы к взаимной блокировке на собственных
же строках.

**Перезапуск при конфликте.** Транзакция целиком повторяется при `40001`
(serialization_failure) и `40P01` (deadlock_detected). Повторы применяются только
к внешней транзакции — перезапускать одну точку сохранения бессмысленно, конфликт
находится уровнем выше.

```go
err := pool.InTx(ctx, fn,
    postgres.WithIsolationLevel(pgx.Serializable),
    postgres.WithTxRetries(5),
)
```

## Повторы запросов

Одиночный запрос повторяется **только** когда `pgconn.SafeToRetry(err)` подтверждает,
что он не дошёл до сервера. Это единственный случай, когда повтор гарантированно
не выполнит операцию дважды.

Политику можно расширить, но осознанно:

```go
postgres.WithRetryPolicy(postgres.RetryOn(postgres.SQLStateLockNotAvailable))
```

Повтор запроса, который мог выполниться, продублирует операцию. Для конфликтов
сериализации повторять надо транзакцию целиком — это уже делает `InTx`.

## Таймауты

Отмена по `context` **не гарантирует**, что PostgreSQL прекратит выполнение: pgx
отправит запрос на отмену, но это гонка, и тяжёлый запрос может дожигать CPU уже
никому не нужным. Надёжная защита — серверные таймауты:

```bash
DB_STATEMENT_TIMEOUT=30s
DB_LOCK_TIMEOUT=5s
DB_IDLE_IN_TX_SESSION_TIMEOUT=60s
```

## Массовые операции

```go
// COPY — самый быстрый способ вставки, на порядок быстрее INSERT.
// Ограничение протокола: ON CONFLICT не поддерживается.
copied, err := postgres.CopyFrom(ctx, pool, "users", []string{"name", "email"}, users,
    func(u User) ([]any, error) { return []any{u.Name, u.Email}, nil })

// Пачка запросов за один сетевой обмен — лекарство от N+1.
err := postgres.BulkExec(ctx, pool, `INSERT INTO users (name, email) VALUES ($1, $2)`, args)
```

Нужен upsert большого объёма — залейте данные во временную таблицу через `CopyFrom`,
затем `INSERT ... SELECT ... ON CONFLICT DO UPDATE`.

## Query builder

Для динамических условий подключён [squirrel](https://github.com/Masterminds/squirrel),
преднастроенный на `$1`-плейсхолдеры:

```go
query := postgres.Builder().Select("id", "name").From("users")
if filter.Active {
    query = query.Where(sq.Eq{"active": true})
}

users, err := postgres.SelectBy[User](ctx, pool, query)
```

`RunWith` не используйте: запросы должен выполнять пул коннектора, иначе они пройдут
мимо трейсинга, метрик и повторов.

## Дополнительные возможности

### Advisory locks

Распределённый мьютекс средствами самой базы — без Redis и etcd:

```go
err := pool.WithAdvisoryLock(ctx, postgres.AdvisoryKey("cleanup.job"), func(ctx context.Context) error {
    return runCleanup(ctx)
})
if errors.Is(err, postgres.ErrLockNotAcquired) {
    // задачу уже выполняет другой экземпляр сервиса
}
```

Блокировка привязана к соединению, поэтому на время удержания оно изымается из пула.
Не удерживайте её дольше необходимого.

### LISTEN/NOTIFY

```go
listener, err := pool.Listen(ctx, "user_events")
defer listener.Close()

for notification := range listener.Notifications() {
    handle(notification.Payload)
}
```

Работает на выделенном соединении вне пула, при обрыве переподключается сам.

⚠️ Подписка живёт ровно столько, сколько существует её соединение с PostgreSQL. Если
между сервисом и базой стоит промежуточный слой, переиспользующий соединения между
клиентами, уведомления до подписчика не дойдут — нужно прямое подключение.

### Разделение чтения и записи

```bash
DB_REPLICA_HOSTS=replica1,replica2
```

```go
users, err := postgres.Select[User](postgres.ReadOnly(ctx), pool, `SELECT * FROM users`)
```

Реплика отстаёт от мастера, поэтому чтение сразу после записи может не увидеть
записанное. Не используйте `ReadOnly` в сценариях «записал и тут же прочитал».

Внутри транзакции пометка запрещена и вернёт `ErrReadOnlyInTx`: транзакция обязана
целиком выполняться на мастере, и уводить её на реплику молча нельзя.

### Healthcheck

```go
if err := pool.Healthcheck(ctx); err != nil {
    // не готов принимать трафик
}
```

Проверяет не только доступность базы, но и исчерпание пула: иначе под считается живым
ровно в тот момент, когда все соединения заняты и запросы стоят в очереди.

### Миграции

Отдельный подпакет — кто не импортирует, тот не платит за зависимость goose:

```go
import "github.com/arrowwhi/infrastructure/connections/postgres/migrate"

//go:embed migrations/*.sql
var migrations embed.FS

err := migrate.Up(ctx, pool, migrations, "migrations")
```

Миграции выполняются под advisory-блокировкой, поэтому одновременный старт нескольких
подов безопасен: применит ровно один, остальные подождут.

## Failover и подключение

`Host` принимает список хостов через запятую — это возможность libpq, унаследованная pgx:

```bash
DB_HOST=pg1,pg2,pg3
DB_TARGET_SESSION_ATTRS=read-write
```

Драйвер сам переберёт хосты и выберет живой мастер. Переключение при failover
Patroni/Stolon работает без единой строки нашего кода.

Можно передать и готовую строку целиком — удобно для managed-баз:

```bash
DB_DSN=postgres://user:pass@host:5432/db?sslmode=verify-full
```

### TLS

Простой случай — `DB_SSLMODE=verify-full`. Для mTLS и для k8s-секретов без файлов
на диске есть полный `TLSConfig` с теми же полями, что в `http/server`: сертификаты
можно передать байтами, в том числе в base64.

### Ротация секретов

Пароли от Vault и IAM-токены живут минуты. Хук вызывается перед созданием каждого
соединения, поэтому пул подтянет свежий секрет сам, без рестарта пода:

```go
postgres.WithBeforeConnect(func(ctx context.Context, cc *pgx.ConnConfig) error {
    token, err := vault.Token(ctx)
    if err != nil {
        return err
    }

    cc.Password = token

    return nil
})
```

## Режим выполнения запросов

По умолчанию используется штатный для pgx режим `cache_statement`: запросы готовятся
как именованные prepared statements и кешируются на соединении. Это самый быстрый
вариант, и менять его без причины не нужно.

Если окружение по какой-то причине не переносит именованные prepared statements —
характерный симптом — ошибки вида `prepared statement "lrupsc_1_0" already exists`,
режим переключается:

```bash
DB_QUERY_EXEC_MODE=simple_protocol
```

Цена — потеря бинарного протокола и заметное падение производительности, поэтому
переключать стоит только при реальных симптомах.

Доступные значения: `cache_statement`, `cache_describe`, `describe_exec`, `exec`,
`simple_protocol`.

## Обработка ошибок

```go
switch {
case postgres.IsNoRows(err):
case postgres.IsUniqueViolation(err):
case postgres.IsQueryCanceled(err):   // сработал statement_timeout
}
```

`postgres.SQLState(err)` возвращает код ошибки PostgreSQL для случаев,
не покрытых готовыми предикатами.

## Как посмотреть трейсы и метрики вживую

В репозитории есть работающий стенд: `examples/http` подключён к PostgreSQL, а его ручки
делают настоящие запросы к базе.

```bash
make obs-up      # PostgreSQL, OTel Collector, Jaeger, Prometheus, Grafana
make run-server  # сервис примера на :8080

make test-endpoints   # разовые запросы
make load-test        # нагрузка на 30 секунд — с ней графики оживают
```

Ручки, которые ходят в базу:

| Запрос | Что делает в PostgreSQL |
|---|---|
| `GET /api/users/123` | `SELECT` пользователя |
| `GET /api/users/999` | Тот же `SELECT`, но строк нет → `IsNoRows` → 404 |
| `POST /api/orders` | Транзакция: `BEGIN` → `SELECT ... FOR UPDATE` → `INSERT` → `COMMIT` |
| `GET /readiness` | `Healthcheck` со статистикой пула |

### Трейсы — Jaeger, http://localhost:16686

1. В поле **Service** выберите `http-server-example`.
2. В **Operation** — `POST /api/orders`, это самый показательный запрос.
3. **Find Traces** и откройте любой трейс.

Внутри увидите дерево, где обращения к базе вложены в span ручки:

```
POST /api/orders                                              87.3ms
└─ createOrder                                                87.2ms
   ├─ validateOrder                                           51.0ms
   ├─ query begin                                              1.6ms
   ├─ query SELECT id FROM users WHERE id = $1 FOR UPDATE      1.0ms
   ├─ query INSERT INTO orders (...) RETURNING id              0.9ms
   ├─ query commit                                             1.9ms
   └─ sendNotification                                        30.7ms
```

Полезное внутри span'а базы: `db.statement` — текст запроса (аргументов нет никогда),
`pgx.rows_affected`, а при ошибке — `pgx.sql_state` с кодом PostgreSQL.

Если span'ы базы оказались **рядом** с ручкой, а не внутри неё, значит где-то потерян
контекст: `_, span := tracer.Start(ctx, ...)` вместо `ctx, span := tracer.Start(ctx, ...)`.
Контекст из `Start` обязательно нужно переприсваивать, иначе связь родитель-потомок рвётся.

### Метрики — Grafana, http://localhost:3000 (admin/admin)

Дашборд **PostgreSQL Connector** (`/d/postgres-connector`) провижинится автоматически.
Двенадцать панелей:

- запросы и ошибки в секунду, занятые соединения и предел пула — плитками сверху;
- интенсивность и p95 **по именам запросов** — здесь виден конкретный медленный запрос;
- p50/p95/p99 по всей базе;
- ошибки с разбивкой по SQLSTATE (23505 отделён от 08*);
- соединения пула, ожидание соединения, время его получения, жизненный цикл соединений.

Метрики в Prometheus называются `otel_db_client_operation_duration_seconds_*`,
`otel_db_client_operation_errors_total` и `otel_pgxpool_*` (префикс `otel_` добавляет
экспортёр коллектора).

Если панели пустые — проверьте, что инструментация включена: в `Config` примера
`TracingEnabled` и `MetricsEnabled` выставлены в `true` явно, поскольку в самом
коннекторе они выключены по умолчанию.

## Тестирование

Пакет покрыт интеграционными тестами на [testcontainers-go](https://golang.testcontainers.org/) —
они поднимают настоящий PostgreSQL в Docker. Иначе поведение транзакций, SQLSTATE,
`COPY` и serialization failure проверить нечем.

```bash
go test ./connections/postgres/...            # нужен Docker
go test ./connections/postgres/... -short     # только юнит-тесты
```
