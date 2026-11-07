package server

import (
	"context"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/rs/zerolog"

	"github.com/arrowwhi/infrastructure/observability/logger"
)

// interceptorLogger адаптирует глобальный логгер (observability/logger) к интерфейсу
// go-grpc-middleware, сохраняя извлечение полей из контекста и маскирование.
func interceptorLogger() logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		var event *zerolog.Event

		switch lvl {
		case logging.LevelDebug:
			event = logger.Debug(ctx)
		case logging.LevelWarn:
			event = logger.Warn(ctx)
		case logging.LevelError:
			event = logger.Error(ctx)
		default:
			event = logger.Info(ctx)
		}

		for i := 0; i+1 < len(fields); i += 2 {
			if key, ok := fields[i].(string); ok {
				event = event.Interface(key, fields[i+1])
			}
		}

		event.Msg(msg)
	})
}
