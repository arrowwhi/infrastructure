# telegram/api (устарел)

Код переехал в пакет [`telegram`](../README.md#клиент-bot-api) (раздел «клиент Bot API»). Этот пакет оставлен
для обратной совместимости: внутри только алиасы типов и функции-обёртки, поэтому старый код
собирается без изменений, а значения свободно передаются между старым и новым путём.

Переезд:

```go
import "github.com/arrowwhi/infrastructure/telegram"
```

Имена: `Config` → `telegram.ClientConfig`, `DefaultConfig` → `telegram.DefaultClientConfig`, `Option` → `telegram.ClientOption`, `New` → `telegram.NewClient`, `Error` → `telegram.APIError`; остальные имена те же.
