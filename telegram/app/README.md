# telegram/app (устарел)

Код переехал в пакет [`telegram`](../README.md#запуск-сервиса) (раздел «запуск сервиса»). Этот пакет оставлен
для обратной совместимости: внутри только алиасы типов и функции-обёртки, поэтому старый код
собирается без изменений, а значения свободно передаются между старым и новым путём.

Переезд:

```go
import "github.com/arrowwhi/infrastructure/telegram"
```

Имена: `Config` → `telegram.AppConfig`, `New` → `telegram.NewApp`; остальные имена (`App`, `Component`, `ComponentFunc`, `AdminConfig`, `LoadConfig`) те же.
