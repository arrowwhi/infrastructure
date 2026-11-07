package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// ---- Test Helpers ----

type ctxKey string

type stry string

func (s stry) String() string { return string(s) }

type stubMasker struct {
	out []byte
	err error
}

func (s stubMasker) Mask(p []byte) ([]byte, error) {
	return s.out, s.err
}

func readJSON(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(buf).Decode(&m); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	return m
}

func withFixedTime(t *testing.T, ts time.Time) {
	t.Helper()
	prev := zerolog.TimestampFunc
	zerolog.TimestampFunc = func() time.Time { return ts }
	t.Cleanup(func() { zerolog.TimestampFunc = prev })
}

func resetGlobal() {
	global = nil
	once = sync.Once{}
	contextExtractor = nil
}

// ---- Config Tests ----

func TestConfig_GetZerologLevel(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		expected zerolog.Level
	}{
		{"trace", "trace", zerolog.TraceLevel},
		{"debug", "debug", zerolog.DebugLevel},
		{"info", "info", zerolog.InfoLevel},
		{"warn", "warn", zerolog.WarnLevel},
		{"error", "error", zerolog.ErrorLevel},
		{"fatal", "fatal", zerolog.FatalLevel},
		{"invalid", "invalid", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Level: tt.level}
			if got := cfg.GetZerologLevel(); got != tt.expected {
				t.Errorf("GetZerologLevel() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---- Logger Initialization Tests ----

func TestInit_OnlyOnce(t *testing.T) {
	resetGlobal()
	defer resetGlobal()

	cfg1 := &Config{Level: "debug"}
	cfg2 := &Config{Level: "error"}

	Init(cfg1, "", "", "")
	first := global

	Init(cfg2, "", "", "")
	second := global

	if first != second {
		t.Error("Init() should only initialize once")
	}
}

func TestGetGlobal_DefaultWhenNotInitialized(t *testing.T) {
	resetGlobal()
	defer resetGlobal()

	l := getGlobal()
	if l == nil {
		t.Error("getGlobal() should return default logger when not initialized")
	}
}

func TestNew_WithVariousConfigs(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{"basic", &Config{Level: "info"}},
		{"debug with trace", &Config{Level: "debug", Stacktrace: true}},
		{"pretty output", &Config{Level: "info", PrettyOutput: true}},
		{"all options", &Config{Level: "warn", Stacktrace: true, PrettyOutput: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.config, "", "", "")
			if l == nil {
				t.Fatal("New() returned nil")
			}
			if l.traceEnabled != tt.config.Stacktrace {
				t.Errorf("traceEnabled = %v, want %v", l.traceEnabled, tt.config.Stacktrace)
			}
			if l.prettyOutput != tt.config.PrettyOutput {
				t.Errorf("prettyOutput = %v, want %v", l.prettyOutput, tt.config.PrettyOutput)
			}
		})
	}
}

func TestNew_WithAppVersionEnv(t *testing.T) {
	tests := []struct {
		name        string
		app         string
		version     string
		environment string
		wantFields  map[string]string
	}{
		{
			name:        "all fields",
			app:         "test-app",
			version:     "1.0.0",
			environment: "production",
			wantFields:  map[string]string{"app": "test-app", "version": "1.0.0", "env": "production"},
		},
		{
			name:        "only app",
			app:         "my-service",
			version:     "",
			environment: "",
			wantFields:  map[string]string{"app": "my-service"},
		},
		{
			name:        "empty fields",
			app:         "",
			version:     "",
			environment: "",
			wantFields:  map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &Config{Level: "info"}

			// Create logger manually with custom output
			logger := zerolog.New(&buf)
			ctx := logger.With().Timestamp()

			if tt.app != "" {
				ctx = ctx.Str("app", tt.app)
			}
			if tt.version != "" {
				ctx = ctx.Str("version", tt.version)
			}
			if tt.environment != "" {
				ctx = ctx.Str("env", tt.environment)
			}

			l := &Logger{
				logger:       ctx.Logger().Level(cfg.GetZerologLevel()),
				traceEnabled: cfg.Stacktrace,
				prettyOutput: cfg.PrettyOutput,
			}

			resetGlobal()
			global = l

			Info(context.TODO()).Msg("test")

			output := buf.String()
			for k, v := range tt.wantFields {
				expectedField := `"` + k + `":"` + v + `"`
				if !strings.Contains(output, expectedField) {
					t.Errorf("expected field %s in output, got: %s", expectedField, output)
				}
			}
		})
	}
}

// ---- Logging Level Tests ----

func TestLoggingLevels(t *testing.T) {
	tests := []struct {
		name     string
		logFunc  func(context.Context) *zerolog.Event
		expected string
	}{
		{"debug", Debug, "debug"},
		{"info", Info, "info"},
		{"warn", Warn, "warn"},
		{"error", Error, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf).With().Timestamp().Logger()
			resetGlobal()
			defer resetGlobal()
			global = &Logger{logger: zl}

			tt.logFunc(context.TODO()).Msg("test")

			m := readJSON(t, &buf)
			if m["level"] != tt.expected {
				t.Errorf("level = %v, want %v", m["level"], tt.expected)
			}
			if m["message"] != "test" && m["msg"] != "test" {
				t.Errorf("message not found in output")
			}
		})
	}
}

func TestLoggingLevels_WithContext(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	SetContextKeys("user_id")
	ctx := context.WithValue(context.Background(), "user_id", "123")

	Info(ctx).Msg("test")

	m := readJSON(t, &buf)
	if m["user_id"] != "123" {
		t.Errorf("user_id = %v, want 123", m["user_id"])
	}
}

// ---- Field Tests ----

func TestEvent_Fields(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	tests := []struct {
		name     string
		logFunc  func()
		key      string
		expected any
	}{
		{
			"string field",
			func() { Info(context.TODO()).Str("name", "john").Msg("test") },
			"name",
			"john",
		},
		{
			"int field",
			func() { Info(context.TODO()).Int("count", 42).Msg("test") },
			"count",
			float64(42),
		},
		{
			"bool field",
			func() { Info(context.TODO()).Bool("active", true).Msg("test") },
			"active",
			true,
		},
		{
			"error field",
			func() { Error(context.TODO()).Err(errors.New("boom")).Msg("test") },
			"error",
			"boom",
		},
		{
			"multiple fields",
			func() { Info(context.TODO()).Str("a", "1").Int("b", 2).Msg("test") },
			"a",
			"1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()
			m := readJSON(t, &buf)
			if m[tt.key] != tt.expected {
				t.Errorf("%s = %v, want %v", tt.key, m[tt.key], tt.expected)
			}
		})
	}
}

func TestEvent_FieldOverride(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	Info(context.TODO()).Str("key", "first").Str("key", "second").Msg("test")

	m := readJSON(t, &buf)
	if m["key"] != "second" {
		t.Errorf("key = %v, want second", m["key"])
	}
}

// ---- Context Extractor Tests ----

func TestSetContextExtractor(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	SetContextExtractor(func(evt *zerolog.Event, ctx context.Context) *zerolog.Event {
		if v := ctx.Value("custom"); v != nil {
			return evt.Str("custom", v.(string))
		}
		return evt
	})

	ctx := context.WithValue(context.Background(), "custom", "value")
	Info(ctx).Msg("test")

	m := readJSON(t, &buf)
	if m["custom"] != "value" {
		t.Errorf("custom = %v, want value", m["custom"])
	}
}

func TestSetContextKeys(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	SetContextKeys("req_id", "user_id")
	ctx := context.Background()
	ctx = context.WithValue(ctx, "req_id", "abc123")
	ctx = context.WithValue(ctx, "user_id", "user456")

	Info(ctx).Msg("test")

	m := readJSON(t, &buf)
	if m["req_id"] != "abc123" {
		t.Errorf("req_id = %v, want abc123", m["req_id"])
	}
	if m["user_id"] != "user456" {
		t.Errorf("user_id = %v, want user456", m["user_id"])
	}
}

func TestSetContextKeys_EmptyKey(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	SetContextKeys("", "valid")
	ctx := context.WithValue(context.Background(), "valid", "ok")

	Info(ctx).Msg("test")

	m := readJSON(t, &buf)
	if m["valid"] != "ok" {
		t.Errorf("valid = %v, want ok", m["valid"])
	}
}

func TestSetContextKeys_NilContext(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	SetContextKeys("key")
	Info(context.TODO()).Msg("test")

	m := readJSON(t, &buf)
	if _, exists := m["key"]; exists {
		t.Error("key should not exist when context is nil")
	}
}

// ---- WithContext Tests ----

func TestWithContext_AllTypes(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	ctx := context.Background()
	ctx = context.WithValue(ctx, "str", "string")
	ctx = context.WithValue(ctx, "bool", true)
	ctx = context.WithValue(ctx, "int", 123)
	ctx = context.WithValue(ctx, "int64", int64(456))
	ctx = context.WithValue(ctx, "uint64", uint64(789))
	ctx = context.WithValue(ctx, "float64", float64(3.14))
	ctx = context.WithValue(ctx, "bytes", []byte("data"))
	ctx = context.WithValue(ctx, "err", errors.New("error"))
	ctx = context.WithValue(ctx, "stringer", stry("stringer"))

	logCtx := WithContext(ctx, "str", "bool", "int", "int64", "uint64", "float64", "bytes", "err", "stringer")
	logger := logCtx.Logger()
	logger.Info().Msg("test")

	m := readJSON(t, &buf)

	checks := map[string]func(any) bool{
		"str":      func(v any) bool { return v == "string" },
		"bool":     func(v any) bool { return v == true },
		"int":      func(v any) bool { return v == float64(123) },
		"int64":    func(v any) bool { return v == float64(456) },
		"uint64":   func(v any) bool { return v == float64(789) },
		"float64":  func(v any) bool { return v == 3.14 },
		"bytes":    func(v any) bool { return v == "data" },
		"err":      func(v any) bool { return v == "error" },
		"stringer": func(v any) bool { return v == "stringer" },
	}

	for key, check := range checks {
		if !check(m[key]) {
			t.Errorf("%s = %v, check failed", key, m[key])
		}
	}
}

func TestWithContext_NilContext(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	logCtx := WithContext(context.TODO(), "key")
	logger := logCtx.Logger()
	logger.Info().Msg("test")

	// Should not panic
	_ = readJSON(t, &buf)
}

func TestWithContext_EmptyKeys(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	ctx := context.WithValue(context.Background(), "key", "value")
	logCtx := WithContext(ctx)
	logger := logCtx.Logger()
	logger.Info().Msg("test")

	m := readJSON(t, &buf)
	if _, exists := m["key"]; exists {
		t.Error("key should not exist when no keys specified")
	}
}

func TestWithContext_EmptyKey(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	ctx := context.WithValue(context.Background(), "valid", "ok")
	logCtx := WithContext(ctx, "", "valid")
	logger := logCtx.Logger()
	logger.Info().Msg("test")

	m := readJSON(t, &buf)
	if m["valid"] != "ok" {
		t.Errorf("valid = %v, want ok", m["valid"])
	}
}

func TestWithContext_MissingValue(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	ctx := context.Background()
	logCtx := WithContext(ctx, "missing")
	logger := logCtx.Logger()
	logger.Info().Msg("test")

	m := readJSON(t, &buf)
	if _, exists := m["missing"]; exists {
		t.Error("missing key should not exist")
	}
}

// ---- With Tests ----

func TestWith(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	logger := With().Str("service", "test").Logger()
	logger.Info().Msg("message")

	m := readJSON(t, &buf)
	if m["service"] != "test" {
		t.Errorf("service = %v, want test", m["service"])
	}
}

// ---- Masking Writer Tests ----

func TestNewMaskingWriter_NilNext(t *testing.T) {
	w, err := newMaskingWriter(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w != nil {
		t.Error("expected nil writer when next is nil")
	}
}

//nolint:gocognit
func TestMaskingWriter_Write(t *testing.T) {
	tests := []struct {
		name      string
		maskOut   []byte
		maskErr   error
		wantOut   string
		wantErr   bool
		wantNZero bool
	}{
		{
			name:      "adds newline when absent",
			maskOut:   []byte("hello"),
			wantOut:   "hello\n",
			wantNZero: true,
		},
		{
			name:      "preserves existing newline",
			maskOut:   []byte("hello\n"),
			wantOut:   "hello\n",
			wantNZero: true,
		},
		{
			name:      "empty after mask",
			maskOut:   []byte(""),
			wantOut:   "",
			wantNZero: false,
		},
		{
			name:      "mask error",
			maskErr:   errors.New("mask failed"),
			wantOut:   "",
			wantErr:   true,
			wantNZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			mw := &maskingWriter{
				next:   &out,
				masker: stubMasker{out: tt.maskOut, err: tt.maskErr},
			}

			n, err := mw.Write([]byte("ignored"))

			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				if out.Len() != 0 {
					t.Error("expected no writes on error")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.wantNZero && n == 0 {
				t.Error("expected non-zero bytes written")
			}

			if got := out.String(); got != tt.wantOut {
				t.Errorf("output = %q, want %q", got, tt.wantOut)
			}
		})
	}
}

// ---- Value Type Conversion Tests ----

func TestAddValueToEvent_UnknownType(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	type custom struct {
		Value string
	}

	SetContextExtractor(func(evt *zerolog.Event, ctx context.Context) *zerolog.Event {
		if v := ctx.Value("custom"); v != nil {
			return addValueToEvent(evt, "custom", v)
		}
		return evt
	})

	ctx := context.WithValue(context.Background(), "custom", custom{Value: "test"})
	Info(ctx).Msg("test")

	m := readJSON(t, &buf)
	// Unknown types should use Interface which will serialize as object
	if m["custom"] == nil {
		t.Error("custom field should exist")
	}
}

func TestAddValueToContext_UnknownType(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	type custom struct {
		Value string
	}

	ctx := context.WithValue(context.Background(), "custom", custom{Value: "test"})
	logCtx := WithContext(ctx, "custom")
	logger := logCtx.Logger()
	logger.Info().Msg("test")

	m := readJSON(t, &buf)
	if m["custom"] == nil {
		t.Error("custom field should exist")
	}
}

// ---- Integration Tests ----

func TestIntegration_CompleteFlow(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl}

	withFixedTime(t, time.Unix(1234567890, 0).UTC())

	SetContextKeys("request_id", "user_id")

	ctx := context.Background()
	ctx = context.WithValue(ctx, "request_id", "req-123")
	ctx = context.WithValue(ctx, "user_id", "user-456")

	Info(ctx).Str("action", "login").Bool("success", true).Msg("user logged in")

	m := readJSON(t, &buf)

	if m["level"] != "info" {
		t.Errorf("level = %v, want info", m["level"])
	}
	if m["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want req-123", m["request_id"])
	}
	if m["user_id"] != "user-456" {
		t.Errorf("user_id = %v, want user-456", m["user_id"])
	}
	if m["action"] != "login" {
		t.Errorf("action = %v, want login", m["action"])
	}
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}

	msg := m["message"]
	if msg == nil {
		msg = m["msg"]
	}
	if msg != "user logged in" {
		t.Errorf("message = %v, want 'user logged in'", msg)
	}
}

func TestIntegration_ErrorLogging(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).With().Timestamp().Logger()
	resetGlobal()
	defer resetGlobal()
	global = &Logger{logger: zl, traceEnabled: false}

	err := errors.New("something went wrong")
	Error(context.TODO()).Err(err).Str("component", "database").Msg("failed to connect")

	m := readJSON(t, &buf)

	if m["level"] != "error" {
		t.Errorf("level = %v, want error", m["level"])
	}
	if m["error"] != "something went wrong" {
		t.Errorf("error = %v, want 'something went wrong'", m["error"])
	}
	if m["component"] != "database" {
		t.Errorf("component = %v, want database", m["component"])
	}
}

func TestIntegration_PrettyOutput(t *testing.T) {
	var buf bytes.Buffer
	resetGlobal()
	defer resetGlobal()

	// Save original consoleWriter.Out
	origOut := consoleWriter.Out
	defer func() { consoleWriter.Out = origOut }()

	// Redirect console output to buffer before creating logger
	consoleWriter.Out = &buf

	// Create logger with pretty output
	cfg := &Config{
		Level:        "info",
		PrettyOutput: true,
	}
	global = New(cfg, "", "", "")

	Info(context.TODO()).Str("key", "value").Msg("test message")

	output := buf.String()

	// Pretty output should contain the message in a human-readable format
	if !strings.Contains(output, "test message") {
		t.Errorf("pretty output should contain the message, got: %s", output)
	}
	// Pretty output should NOT be JSON
	if strings.HasPrefix(output, "{") {
		t.Error("pretty output should not be JSON")
	}
}

func TestIntegration_DifferentLogLevels(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		shouldLog map[string]bool
	}{
		{
			name:  "debug level logs all",
			level: "debug",
			shouldLog: map[string]bool{
				"debug": true,
				"info":  true,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:  "info level skips debug",
			level: "info",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  true,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:  "warn level skips debug and info",
			level: "warn",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  false,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:  "error level only logs errors",
			level: "error",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  false,
				"warn":  false,
				"error": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &Config{Level: tt.level}
			zl := zerolog.New(&buf).With().Timestamp().Logger().Level(cfg.GetZerologLevel())
			resetGlobal()
			defer resetGlobal()
			global = &Logger{logger: zl}

			Debug(context.TODO()).Msg("debug")
			debugLogged := buf.Len() > 0
			buf.Reset()

			Info(context.TODO()).Msg("info")
			infoLogged := buf.Len() > 0
			buf.Reset()

			Warn(context.TODO()).Msg("warn")
			warnLogged := buf.Len() > 0
			buf.Reset()

			Error(context.TODO()).Msg("error")
			errorLogged := buf.Len() > 0

			if debugLogged != tt.shouldLog["debug"] {
				t.Errorf("debug logged = %v, want %v", debugLogged, tt.shouldLog["debug"])
			}
			if infoLogged != tt.shouldLog["info"] {
				t.Errorf("info logged = %v, want %v", infoLogged, tt.shouldLog["info"])
			}
			if warnLogged != tt.shouldLog["warn"] {
				t.Errorf("warn logged = %v, want %v", warnLogged, tt.shouldLog["warn"])
			}
			if errorLogged != tt.shouldLog["error"] {
				t.Errorf("error logged = %v, want %v", errorLogged, tt.shouldLog["error"])
			}
		})
	}
}
