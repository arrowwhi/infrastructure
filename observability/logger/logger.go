// Package logger provides structured logging functionality using zerolog with context extraction and masking support.
package logger

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/rs/zerolog"
)

type Logger struct {
	logger       zerolog.Logger
	traceEnabled bool
	prettyOutput bool
}

type ContextExtractor func(*zerolog.Event, context.Context) *zerolog.Event

var (
	global           *Logger
	once             sync.Once
	contextExtractor ContextExtractor
)

// Init initializes the global logger instance (called only once)
func Init(config *Config, app, version, environment string) {
	once.Do(func() {
		global = New(config, app, version, environment)
	})
}

// getGlobal returns the global logger instance
func getGlobal() *Logger {
	if global == nil {
		// Return a default logger if not initialized
		return New(&Config{Level: "debug"}, "", "", "")
	}

	return global
}

// SetContextExtractor sets the function that extracts fields from context
func SetContextExtractor(fn ContextExtractor) {
	contextExtractor = fn
}

// SetContextKeys creates a context extractor from a list of keys to extract from context
func SetContextKeys(keys ...string) {
	contextExtractor = func(evt *zerolog.Event, ctx context.Context) *zerolog.Event {
		for _, key := range keys {
			if key == "" {
				continue
			}
			if v := ctx.Value(key); v != nil {
				evt = addValueToEvent(evt, key, v)
			}
		}

		return evt
	}
}

// Package-level logging functions

func Info(ctx context.Context) *zerolog.Event {
	l := getGlobal()
	evt := l.logger.Info()
	if ctx != nil && contextExtractor != nil {
		evt = contextExtractor(evt, ctx)
	}

	return evt
}

func Warn(ctx context.Context) *zerolog.Event {
	l := getGlobal()
	evt := l.logger.Warn()
	if ctx != nil && contextExtractor != nil {
		evt = contextExtractor(evt, ctx)
	}

	return evt
}

func Debug(ctx context.Context) *zerolog.Event {
	l := getGlobal()
	evt := l.logger.Debug()
	if ctx != nil && contextExtractor != nil {
		evt = contextExtractor(evt, ctx)
	}

	return evt
}

func Error(ctx context.Context) *zerolog.Event {
	l := getGlobal()
	evt := l.logger.Error()
	if ctx != nil && contextExtractor != nil {
		evt = contextExtractor(evt, ctx)
	}

	return evt
}

func Fatal(ctx context.Context) *zerolog.Event {
	l := getGlobal()
	evt := l.logger.Fatal()
	if ctx != nil && contextExtractor != nil {
		evt = contextExtractor(evt, ctx)
	}

	return evt
}

func With() zerolog.Context {
	l := getGlobal()
	return l.logger.With()
}

func WithContext(ctx context.Context, keys ...string) zerolog.Context {
	l := getGlobal()
	zctx := l.logger.With()

	if ctx == nil || len(keys) == 0 {
		return zctx
	}

	for _, k := range keys {
		if k == "" {
			continue
		}
		if v := ctx.Value(k); v != nil {
			zctx = addValueToContext(zctx, k, v)
		}
	}

	return zctx
}

func New(config *Config, app, version, environment string) *Logger {
	var out io.Writer = os.Stdout
	if config.PrettyOutput {
		out = consoleWriter
	}

	if config.Masking {
		mw, err := newMaskingWriter(out)
		if err != nil {
			panic("newMaskingWriter: " + err.Error())
		}
		out = mw
	}

	logger := zerolog.New(out)
	ctx := logger.With().Timestamp()

	if app != "" {
		ctx = ctx.Str("app", app)
	}
	if version != "" {
		ctx = ctx.Str("version", version)
	}
	if environment != "" {
		ctx = ctx.Str("env", environment)
	}

	logger = ctx.Logger()

	return &Logger{
		logger:       logger.Level(config.GetZerologLevel()),
		traceEnabled: config.Stacktrace,
		prettyOutput: config.PrettyOutput,
	}
}

func addValueToContext(zctx zerolog.Context, key string, v any) zerolog.Context {
	switch t := v.(type) {
	case string:
		return zctx.Str(key, t)
	case bool:
		return zctx.Bool(key, t)
	case int:
		return zctx.Int(key, t)
	case int64:
		return zctx.Int64(key, t)
	case uint64:
		return zctx.Uint64(key, t)
	case float64:
		return zctx.Float64(key, t)
	case []byte:
		return zctx.Bytes(key, t)
	case error:
		return zctx.Str(key, t.Error())
	case interface{ String() string }:
		return zctx.Str(key, t.String())
	default:
		return zctx.Interface(key, v)
	}
}

func addValueToEvent(evt *zerolog.Event, key string, v any) *zerolog.Event {
	switch t := v.(type) {
	case string:
		return evt.Str(key, t)
	case bool:
		return evt.Bool(key, t)
	case int:
		return evt.Int(key, t)
	case int64:
		return evt.Int64(key, t)
	case uint64:
		return evt.Uint64(key, t)
	case float64:
		return evt.Float64(key, t)
	case []byte:
		return evt.Bytes(key, t)
	case error:
		return evt.Str(key, t.Error())
	case interface{ String() string }:
		return evt.Str(key, t.String())
	default:
		return evt.Interface(key, v)
	}
}
