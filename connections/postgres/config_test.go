package postgres

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pgErr собирает серверную ошибку PostgreSQL с заданным SQLSTATE.
func pgErr(code string) error {
	return &pgconn.PgError{Code: code, Message: "test error"}
}

func TestConnStringEscapesPassword(t *testing.T) {
	t.Parallel()

	// Ровно тот случай, на котором ломалась сборка DSN через fmt.Sprintf:
	// спецсимволы пароля выглядели как разделители строки подключения.
	cfg := Config{
		Host:     "db.internal",
		Port:     5432,
		User:     "svc",
		Password: "p@ss w0rd:/?#[]&=",
		Database: "orders",
		SSLMode:  "require",
	}

	parsed, err := url.Parse(cfg.connString())
	require.NoError(t, err)

	password, hasPassword := parsed.User.Password()
	require.True(t, hasPassword)
	assert.Equal(t, "p@ss w0rd:/?#[]&=", password)
	assert.Equal(t, "svc", parsed.User.Username())
	assert.Equal(t, "db.internal:5432", parsed.Host)
	assert.Equal(t, "/orders", parsed.Path)
	assert.Equal(t, "require", parsed.Query().Get("sslmode"))

	// Главная проверка: pgx должен разобрать то, что мы собрали.
	connCfg, err := pgx.ParseConfig(cfg.connString())
	require.NoError(t, err)
	assert.Equal(t, "p@ss w0rd:/?#[]&=", connCfg.Password)
	assert.Equal(t, "orders", connCfg.Database)
}

func TestConnStringUsesExplicitDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		DSN:      "postgres://someone:secret@example:6432/db?sslmode=disable",
		Host:     "ignored",
		User:     "ignored",
		Database: "ignored",
	}

	assert.Equal(t, cfg.DSN, cfg.connString())
}

func TestConnStringMultiHost(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Host:               "pg1,pg2:5433, pg3 ",
		Port:               5432,
		User:               "svc",
		Password:           "pass",
		Database:           "orders",
		TargetSessionAttrs: "read-write",
	}

	connString := cfg.connString()

	// Порт проставляется каждому хосту, у которого его нет, а лишние пробелы отбрасываются.
	parsed, err := url.Parse(connString)
	require.NoError(t, err)
	assert.Equal(t, "pg1:5432,pg2:5433,pg3:5432", parsed.Host)

	// Главное — что перебор хостов достаётся нам от libpq даром: pgx раскладывает
	// список в цепочку fallback'ов, по которой сам находит живой мастер.
	connCfg, err := pgx.ParseConfig(connString)
	require.NoError(t, err)

	assert.Equal(t, "pg1", connCfg.Host)
	assert.Equal(t, uint16(5432), connCfg.Port)

	// target_session_attrs pgx превращает во внутреннюю проверку соединения,
	// поэтому в RuntimeParams его нет — достаточно, что строка разобралась без ошибки.
	assert.Equal(t, "read-write", parsed.Query().Get("target_session_attrs"))

	seen := make(map[string]uint16, len(connCfg.Fallbacks))
	for _, fallback := range connCfg.Fallbacks {
		seen[fallback.Host] = fallback.Port
	}

	assert.Equal(t, uint16(5433), seen["pg2"])
	assert.Equal(t, uint16(5432), seen["pg3"])
}

func TestReplicaConnStringPrefersStandby(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Host:               "master",
		ReplicaHosts:       "replica1,replica2",
		Port:               5432,
		User:               "svc",
		Password:           "pass",
		Database:           "orders",
		TargetSessionAttrs: "read-write",
	}

	parsed, err := url.Parse(cfg.replicaConnString())
	require.NoError(t, err)

	assert.Equal(t, "replica1:5432,replica2:5432", parsed.Host)
	assert.Equal(t, "prefer-standby", parsed.Query().Get("target_session_attrs"))
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg     Config
		wantErr bool
	}{
		"valid": {
			cfg: Config{User: "u", Database: "d", QueryExecMode: QueryExecModeCacheStatement, MaxConns: 10, MinConns: 2},
		},
		"dsn instead of fields": {
			cfg: Config{DSN: "postgres://u:p@h/db"},
		},
		"no user": {
			cfg:     Config{Database: "d"},
			wantErr: true,
		},
		"no database": {
			cfg:     Config{User: "u"},
			wantErr: true,
		},
		"unknown exec mode": {
			cfg:     Config{User: "u", Database: "d", QueryExecMode: "turbo"},
			wantErr: true,
		},
		"min above max": {
			cfg:     Config{User: "u", Database: "d", MaxConns: 2, MinConns: 10},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidConfig)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestParseQueryExecMode(t *testing.T) {
	t.Parallel()

	tests := map[string]pgx.QueryExecMode{
		"":                          pgx.QueryExecModeCacheStatement,
		QueryExecModeCacheStatement: pgx.QueryExecModeCacheStatement,
		QueryExecModeCacheDescribe:  pgx.QueryExecModeCacheDescribe,
		QueryExecModeDescribeExec:   pgx.QueryExecModeDescribeExec,
		QueryExecModeExec:           pgx.QueryExecModeExec,
		QueryExecModeSimpleProtocol: pgx.QueryExecModeSimpleProtocol,
	}

	for input, want := range tests {
		got, err := parseQueryExecMode(input)
		require.NoError(t, err, input)
		assert.Equal(t, want, got, input)
	}

	_, err := parseQueryExecMode("nope")
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRuntimeParams(t *testing.T) {
	t.Parallel()

	cfg := Config{
		StatementTimeout:       3 * time.Second,
		LockTimeout:            1500 * time.Millisecond,
		IdleInTxSessionTimeout: time.Minute,
	}

	assert.Equal(t, map[string]string{
		"statement_timeout":                   "3000",
		"lock_timeout":                        "1500",
		"idle_in_transaction_session_timeout": "60000",
	}, cfg.runtimeParams())

	assert.Empty(t, Config{}.runtimeParams())
}

func TestInstrumented(t *testing.T) {
	t.Parallel()

	assert.False(t, Config{}.instrumented(), "по умолчанию инструментация выключена")
	assert.True(t, Config{TracingEnabled: true}.instrumented())
	assert.True(t, Config{MetricsEnabled: true}.instrumented())
	assert.True(t, Config{SlowQueryLog: true}.instrumented())
}

func TestRedactHidesPassword(t *testing.T) {
	t.Parallel()

	err := errors.New(`failed to connect: postgres://svc:sup3rs3cret@db/orders`)

	redacted := redact(err, "sup3rs3cret")
	assert.NotContains(t, redacted.Error(), "sup3rs3cret")
	assert.Contains(t, redacted.Error(), redactedValue)

	assert.Nil(t, redact(nil, "x"))
	assert.Equal(t, err, redact(err, ""), "без пароля ошибка возвращается как есть")
}

func TestJoinHosts(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "localhost:5432", joinHosts("localhost", 5432))
	assert.Equal(t, "h1:5432,h2:6000", joinHosts("h1,h2:6000", 5432))
	assert.Equal(t, "localhost:5432", joinHosts("", 0), "пустой список даёт значение по умолчанию")
	assert.Equal(t, "h1:5432", joinHosts(" h1 , ", 5432))
}

func TestOperationOf(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "SELECT", operationOf("select * from users"))
	assert.Equal(t, "INSERT", operationOf("\n\tINSERT INTO users VALUES ($1)"))
	assert.Equal(t, "WITH", operationOf("WITH cte AS (SELECT 1) SELECT * FROM cte"))
	assert.Equal(t, "SELECT", operationOf("(SELECT 1)"))
	assert.Equal(t, UnknownQueryName, operationOf("   "))
}

func TestIsTxControl(t *testing.T) {
	t.Parallel()

	// Эти команды pgx отправляет сам, пометить их именем из прикладного кода нельзя,
	// поэтому предупреждать о них — чистый шум в логах.
	for _, sql := range []string{"begin", "commit", "rollback", "savepoint sp_1", "release savepoint sp_1"} {
		assert.True(t, isTxControl(sql), sql)
	}

	for _, sql := range []string{"SELECT 1", "INSERT INTO users VALUES ($1)", "UPDATE users SET name = $1"} {
		assert.False(t, isTxControl(sql), sql)
	}
}

func TestRetryOn(t *testing.T) {
	t.Parallel()

	policy := RetryOn(SQLStateSerializationFailure, SQLStateDeadlockDetected)

	assert.False(t, policy(errors.New("plain error")), "не серверная ошибка не повторяется")
	assert.True(t, policy(pgErr(SQLStateSerializationFailure)))
	assert.False(t, policy(pgErr(SQLStateUniqueViolation)))
}

func TestErrorPredicates(t *testing.T) {
	t.Parallel()

	assert.True(t, IsUniqueViolation(pgErr(SQLStateUniqueViolation)))
	assert.True(t, IsForeignKeyViolation(pgErr(SQLStateForeignKeyViolation)))
	assert.True(t, IsSerializationFailure(pgErr(SQLStateSerializationFailure)))
	assert.True(t, IsDeadlock(pgErr(SQLStateDeadlockDetected)))
	assert.True(t, IsQueryCanceled(pgErr(SQLStateQueryCanceled)))
	assert.True(t, IsRetryableTx(pgErr(SQLStateDeadlockDetected)))
	assert.False(t, IsRetryableTx(pgErr(SQLStateUniqueViolation)))
	assert.True(t, IsNoRows(pgx.ErrNoRows))
	assert.Empty(t, SQLState(errors.New("not a pg error")))
}

func TestQueryNameContext(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	assert.Equal(t, UnknownQueryName, QueryNameFromContext(ctx))

	named := WithQueryName(ctx, "user.get_by_id")
	assert.Equal(t, "user.get_by_id", QueryNameFromContext(named))
}

func TestReadOnlyContext(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	assert.False(t, isReadOnlyRoute(ctx))
	assert.True(t, isReadOnlyRoute(ReadOnly(ctx)))
}

func TestNewCompositeTracerCollapses(t *testing.T) {
	t.Parallel()

	assert.Nil(t, newCompositeTracer(), "без трейсеров pgx не должен получить обёртку")
	assert.Nil(t, newCompositeTracer(nil, nil))

	single := &logTracer{}
	assert.Same(t, single, newCompositeTracer(single), "один трейсер передаётся напрямую, без обёртки")

	both := newCompositeTracer(single, &logTracer{})
	assert.IsType(t, &compositeTracer{}, both)
}

func TestNewLogTracerDisabled(t *testing.T) {
	t.Parallel()

	assert.Nil(t, newLogTracer(false, false, time.Second))
	assert.NotNil(t, newLogTracer(false, true, time.Second))
}

func TestOptionsOverrideConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	resolved := newOptions(&cfg, []Option{WithTracing(), WithMetrics(), WithSlowQueryLog(50 * time.Millisecond)})

	assert.True(t, cfg.TracingEnabled)
	assert.True(t, cfg.MetricsEnabled)
	assert.True(t, cfg.SlowQueryLog)
	assert.Equal(t, 50*time.Millisecond, cfg.SlowQueryThreshold)
	assert.Equal(t, defaultServiceName, resolved.serviceName)

	off := Config{TracingEnabled: true, MetricsEnabled: true}
	newOptions(&off, []Option{WithoutTracing(), WithoutMetrics()})
	assert.False(t, off.TracingEnabled)
	assert.False(t, off.MetricsEnabled)
}

func TestIdentifier(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `"users"`, identifier("users").Sanitize())
	assert.Equal(t, `"public"."users"`, identifier("public.users").Sanitize())
}

func TestAdvisoryKeyIsStable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, AdvisoryKey("migrations"), AdvisoryKey("migrations"))
	assert.NotEqual(t, AdvisoryKey("migrations"), AdvisoryKey("cleanup"))
}
