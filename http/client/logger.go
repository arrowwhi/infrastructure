package httpclient

import "context"

type Level int

const (
	Debug Level = iota
	Info
	Warning
	Error
	Fatal
)

type Logger interface {
	Log(ctx context.Context, level Level, msg string, fields ...any)
}

type LoggerFunc func(ctx context.Context, level Level, msg string, fields ...any)

func (f LoggerFunc) Log(ctx context.Context, level Level, msg string, fields ...any) {
	f(ctx, level, msg, fields...)
}

type noopLogger struct{}

func (n *noopLogger) Log(ctx context.Context, level Level, msg string, fields ...any) {
	// Ничего не делаем
}

// DefaultLogger возвращает дефолтный noop логгер
func DefaultLogger() Logger {
	return &noopLogger{}
}
