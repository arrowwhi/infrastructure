# telegram/state (устарел)

Код переехал в пакет [`telegram`](../README.md#хранилище-шагов-диалога) (раздел «хранилище шагов диалога»). Этот пакет оставлен
для обратной совместимости: внутри только алиасы типов и функции-обёртки, поэтому старый код
собирается без изменений, а значения свободно передаются между старым и новым путём.

Переезд:

```go
import "github.com/arrowwhi/infrastructure/telegram"
```

Имена: `Key` → `telegram.StateKey`, `New` → `telegram.NewState`, `ErrNotFound` → `telegram.ErrStateNotFound`, `Memory`/`Redis`/`Postgres` → `telegram.MemoryStore`/`RedisStore`/`PostgresStore` (конструкторы — `NewMemoryStore`, `NewRedisStore`, `NewPostgresStore`), `PostgresSchema` → `telegram.PostgresStateSchema`, `DefaultRedisPrefix` → `telegram.DefaultRedisStatePrefix`; остальные имена те же.
