package client

import (
	"context"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/rs/zerolog"
)

func interceptorLogger(l *zerolog.Logger) logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		var zlvl zerolog.Level
		switch lvl {
		case logging.LevelDebug:
			zlvl = zerolog.DebugLevel
		case logging.LevelInfo:
			zlvl = zerolog.InfoLevel
		case logging.LevelWarn:
			zlvl = zerolog.WarnLevel
		case logging.LevelError:
			zlvl = zerolog.ErrorLevel
		default:
			zlvl = zerolog.InfoLevel
		}

		event := l.WithLevel(zlvl).Ctx(ctx)

		if len(fields) > 0 {
			// Парсим поля в формате key-value
			for i := 0; i < len(fields)-1; i += 2 {
				if key, ok := fields[i].(string); ok {
					event = event.Interface(key, fields[i+1])
				}
			}
		}

		event.Msg(msg)
	})
}
