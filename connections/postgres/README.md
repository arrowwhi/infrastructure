# connections/postgres

Коннектор к PostgreSQL поверх [pgx v5](https://github.com/jackc/pgx): пул соединений,
трейсинг и метрики OpenTelemetry (выключены по умолчанию и без накладных расходов),
транзакции через контекст с `SAVEPOINT` и перезапуском при конфликте сериализации,
повторы только гарантированно безопасных запросов, серверные таймауты, failover и реплики,
`Healthcheck` для `observability/health`, генерик-хелперы `Get[T]`/`Select[T]`, `COPY`, batch,
query builder, advisory locks и LISTEN/NOTIFY. Миграции — в подпакете [`migrate`](migrate/README.md).

## Подключение

```bash
go get github.com/arrowwhi/infrastructure@latest
```

```go
import "github.com/arrowwhi/infrastructure/connections/postgres"
```

## Пример

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/arrowwhi/infrastructure/connections/postgres"
)

type User struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

func main() {
	ctx := context.Background()

	cfg := postgres.DefaultConfig()
	cfg.Host = "localhost"
	cfg.User = "svc"
	cfg.Password = os.Getenv("DB_PASSWORD")
	cfg.Database = "app"
	cfg.TracingEnabled = true
	cfg.MetricsEnabled = true

	pool, err := postgres.New(ctx, cfg) // проверяет доступность базы
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	ctx = postgres.WithQueryName(ctx, "user.get_by_id") // метка db.query.name в метриках
	user, err := postgres.Get[User](ctx, pool, `SELECT id, name FROM users WHERE id = $1`, 42)
	if postgres.IsNoRows(err) {
		log.Println("not found")
		return
	}
	if err != nil {
		log.Fatal(err)
	}

	err = pool.InTx(ctx, func(ctx context.Context) error {
		_, err := pool.Exec(ctx, `UPDATE users SET name = $1 WHERE id = $2`, user.Name+"!", user.ID)
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

## Конфигурация

`Config` обычно приезжает из общего конфига сервиса (`telegram.LoadConfig`). Без `env-prefix` имена такие:

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `DB_DSN` | — | готовая строка подключения; если задана, поля ниже до `DB_SSLMODE` игнорируются |
| `DB_HOST`, `DB_PORT` | `localhost`, `5432` | хост (можно список через запятую — failover) и порт |
| `DB_USER`, `DB_PASSWORD`, `DB_DATABASE` | — | учётные данные; `DB_USER` и `DB_DATABASE` обязательны без `DB_DSN` |
| `DB_SSLMODE` | `disable` | `disable`, `require`, `verify-ca`, `verify-full` |
| `DB_TARGET_SESSION_ATTRS` | — | `read-write` — перебирать хосты до живого мастера |
| `DB_REPLICA_HOSTS` | — | реплики через запятую для `postgres.ReadOnly(ctx)` |
| `DB_APPLICATION_NAME` | — | `application_name` в `pg_stat_activity` |
| `DB_MAX_CONNS`, `DB_MIN_CONNS` | `20`, `5` | размер пула |
| `DB_MAX_CONN_LIFETIME`, `DB_MAX_CONN_IDLE_TIME` | `30m`, `2m` | ротация соединений |
| `DB_CONNECT_TIMEOUT` | `5s` | установка соединения |
| `DB_STATEMENT_TIMEOUT`, `DB_LOCK_TIMEOUT`, `DB_IDLE_IN_TX_SESSION_TIMEOUT` | — | серверные таймауты |
| `DB_QUERY_EXEC_MODE` | `cache_statement` | см. «Режим выполнения запросов» |
| `DB_TRACING_ENABLED`, `DB_METRICS_ENABLED` | `false`, `false` | инструментация |
| `DB_QUERY_LOG_ENABLED` | `false` | лог каждого запроса (дорого) |
| `DB_SLOW_QUERY_LOG`, `DB_SLOW_QUERY_THRESHOLD` | `false`, `200ms` | лог медленных запросов |
| `DB_HEALTHCHECK_ACQUIRE_THRESHOLD` | `1s` | `Healthcheck` падает, если соединение из пула ждали дольше |
| `DB_TLS_ENABLED`, `DB_TLS_CA_FILE`, `DB_TLS_CERT_FILE`, `DB_TLS_KEY_FILE`, `DB_TLS_SERVER_NAME` | — | TLS/mTLS файлами |
| `DB_TLS_CA_DATA`, `DB_TLS_CERT_DATA`, `DB_TLS_KEY_DATA`, `DB_TLS_BASE64`, `DB_TLS_INSECURE_SKIP_VERIFY` | — | сертификаты содержимым (в т.ч. base64) |

Опции `New`: `WithTracing`/`WithoutTracing`, `WithMetrics`/`WithoutMetrics`, `WithSlowQueryLog(d)`,
`WithQueryLogging`, `WithTracerProvider`, `WithMeterProvider`, `WithAttributes`, `WithServiceName`,
`WithBeforeConnect`/`WithAfterConnect`, `WithRetryPolicy`, `WithQueryTracer`.

## Метрики, трейсы, логи

**По умолчанию выключены.** Когда `TracingEnabled` и `MetricsEnabled` равны `false`, трейсер
не создаётся вовсе — `ConnConfig.Tracer` остаётся `nil`, накладных расходов нет.

- Метрики: `db.client.operation.duration` (с), `db.client.operation.errors` (метка
  `db.response.status_code` — SQLSTATE), `db.client.batch.size`, плюс статистика пула otelpgx
  (`pgxpool.*`). Основные метки — `db.query.name` и `db.operation.name`.
- Трейсы ([otelpgx](https://github.com/exaring/otelpgx), скрыт внутри пакета): спан на запрос, batch,
  `COPY`; `db.statement` — текст SQL. **Аргументы запросов — никогда**:
  параметры содержат персональные данные.
- Логи через `observability/logger` (включаются при `DB_SLOW_QUERY_LOG` или `DB_QUERY_LOG_ENABLED`):
  ошибки запросов — `error` (кроме «нет строк»), медленные — `warn`, все запросы — `debug`
  (нужен `LOG_LEVEL=debug`). При включённых метриках — однократное предупреждение на каждый SQL без имени.

### Имена запросов

Имя задаётся через контекст: `ctx = postgres.WithQueryName(ctx, "user.get_by_id")`. Без имени запрос
попадает в метрики как `db_query_name="unknown"`, а в лог один раз падает предупреждение — по одному
на уникальный текст SQL. Разбивать метрики по тексту SQL нельзя (кардинальность), поэтому имя —
из ограниченного набора: `user.get_by_id`, а не идентификатор конкретной сущности.

## Связь с другими пакетами

- Health: `a.Health().Register("postgres", pool.Healthcheck)` — проверяет и доступность базы,
  и исчерпание пула.
- Остановка: `a.OnShutdown(func(context.Context) error { pool.Close(); return nil })`.
- Трейсы и метрики — в глобальные провайдеры `observability/tracing`/`observability/metrics`
  (или `telegram`); свои — `WithTracerProvider`/`WithMeterProvider`.
- `telegram.NewPostgresStore(pool, "")` — хранилище шагов диалога бота.
- `utils/txmanager` — только для транзакции, общей с Redis; для PostgreSQL достаточно `pool.InTx`.

## Подводные камни

- Метрики без `WithQueryName` почти бесполезны: всё сольётся в `unknown`.
- `ReadOnly(ctx)` уводит запрос на реплику с отставанием — не для «записал и тут же прочитал»;
  внутри транзакции даёт `ErrReadOnlyInTx`.
- `RunWith` у query builder не используйте: запрос пройдёт мимо трейсинга, метрик и повторов.
- Отмена по `ctx` не гарантирует остановку запроса на сервере — задавайте `DB_STATEMENT_TIMEOUT`.
- `WithAdvisoryLock` не ждёт освобождения блокировки, а сразу возвращает `ErrLockNotAcquired`.
- Для `DB_USER`/`DB_PASSWORD`/`DB_DATABASE` стоит тег `required`, который cleanenv не проверяет;
  пустые значения ловит `Config.Validate` при `New` (кроме пароля).
- Гистограмма `db.client.operation.duration` создаётся без явных границ — используются границы OTel
  по умолчанию (0, 5, 10, 25… — рассчитанные на миллисекунды), и почти все запросы попадают в
  первый бакет. До исправления в коде задайте границы View в своём `MeterProvider`.

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

Миграции выполняются под advisory-блокировкой без ожидания: второй одновременно стартующий
экземпляр получит `postgres.ErrLockNotAcquired`. Подробнее — [`migrate`](migrate/README.md).

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
