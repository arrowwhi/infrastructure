package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

// logCtxKey хранит состояние логирования между началом и концом запроса.
type logCtxKey struct{}

// logState — данные, известные на старте запроса.
type logState struct {
	start time.Time
	sql   string
}

// logTracer пишет запросы в observability/logger.
//
// Аргументы запроса не логируются никогда — как и в span'ах. Единая политика важнее
// удобства отладки: параметры содержат персональные данные, и один незакрытый случай
// сводит на нет всё маскирование.
type logTracer struct {
	threshold time.Duration
	logAll    bool
	logSlow   bool
}

var _ pgx.QueryTracer = (*logTracer)(nil)

// newLogTracer создаёт трейсер логирования. Возвращает nil, если логировать нечего.
func newLogTracer(logAll, logSlow bool, threshold time.Duration) *logTracer {
	if !logAll && !logSlow {
		return nil
	}

	return &logTracer{logAll: logAll, logSlow: logSlow, threshold: threshold}
}

func (l *logTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, logCtxKey{}, logState{start: time.Now(), sql: data.SQL})
}

func (l *logTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(logCtxKey{}).(logState)
	if !ok {
		return
	}

	elapsed := time.Since(state.start)
	name := QueryNameFromContext(ctx)

	if data.Err != nil {
		// Отсутствие строк — нормальный исход запроса, а не ошибка уровня error.
		if IsNoRows(data.Err) {
			return
		}

		logger.Error(ctx).
			Err(data.Err).
			Str("query_name", name).
			Str("sql", state.sql).
			Str("sqlstate", SQLState(data.Err)).
			Dur("duration", elapsed).
			Msg("postgres: query failed")

		return
	}

	if l.logSlow && elapsed >= l.threshold {
		logger.Warn(ctx).
			Str("query_name", name).
			Str("sql", state.sql).
			Dur("duration", elapsed).
			Dur("threshold", l.threshold).
			Int64("rows_affected", data.CommandTag.RowsAffected()).
			Msg("postgres: slow query")

		return
	}

	if l.logAll {
		logger.Debug(ctx).
			Str("query_name", name).
			Str("sql", state.sql).
			Dur("duration", elapsed).
			Int64("rows_affected", data.CommandTag.RowsAffected()).
			Msg("postgres: query")
	}
}
