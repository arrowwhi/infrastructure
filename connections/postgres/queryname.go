package postgres

import (
	"context"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

// UnknownQueryName — значение метки для запросов, которым не задали имя.
// Если этих метрик много, значит код не размечен: смотрите warning'и в логе.
const UnknownQueryName = "unknown"

// queryNameCtxKey хранит имя запроса в контексте.
type queryNameCtxKey struct{}

// unnamedWarner предупреждает о неразмеченных запросах ровно один раз на каждый
// уникальный текст SQL. Логировать каждый вызов нельзя: на нагруженном сервисе это
// тысячи одинаковых строк в секунду именно там, где наблюдаемость нужнее всего.
type unnamedWarner struct {
	seen sync.Map
}

// WithQueryName помечает запросы, выполняемые с этим контекстом, стабильным именем.
// Имя становится меткой метрик и полем лога, поэтому оно должно быть из ограниченного
// набора значений: "user.get_by_id", а не идентификатор конкретной сущности.
func WithQueryName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, queryNameCtxKey{}, name)
}

// QueryNameFromContext возвращает имя запроса или UnknownQueryName.
func QueryNameFromContext(ctx context.Context) string {
	name, _ := ctx.Value(queryNameCtxKey{}).(string)
	if name == "" {
		return UnknownQueryName
	}

	return name
}

// warn пишет предупреждение о неразмеченном запросе, если для этого SQL его ещё не было.
func (w *unnamedWarner) warn(ctx context.Context, sql string) {
	key := hashSQL(sql)
	if _, loaded := w.seen.LoadOrStore(key, struct{}{}); loaded {
		return
	}

	logger.Warn(ctx).
		Str("sql", sql).
		Msg("postgres: query has no name, metrics will use query_name=unknown; wrap the context with postgres.WithQueryName")
}

// isTxControl сообщает, что запрос — служебная команда управления транзакцией.
//
// Такие команды pgx отправляет сам (BEGIN при старте транзакции, SAVEPOINT при
// вложенном InTx и так далее), пометить их именем из прикладного кода невозможно,
// поэтому предупреждать о них бессмысленно.
func isTxControl(sql string) bool {
	switch operationOf(sql) {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "END", "ABORT", "PREPARE":
		return true
	default:
		return false
	}
}

// operationOf возвращает тип операции — первое слово запроса в верхнем регистре.
// Множество значений заведомо мало (SELECT/INSERT/UPDATE/...), поэтому метка безопасна
// с точки зрения кардинальности метрик.
func operationOf(sql string) string {
	trimmed := strings.TrimLeft(sql, " \t\r\n(")

	idx := strings.IndexAny(trimmed, " \t\r\n(")
	if idx > 0 {
		trimmed = trimmed[:idx]
	}

	if trimmed == "" {
		return UnknownQueryName
	}

	return strings.ToUpper(trimmed)
}

// hashSQL возвращает компактный ключ для дедупликации предупреждений.
func hashSQL(sql string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sql))

	return h.Sum64()
}
