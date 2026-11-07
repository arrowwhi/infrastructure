package postgres

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// QueryExecModeCacheStatement — режим pgx по умолчанию: именованные prepared statements
	// с кешированием. Самый быстрый вариант, менять его без причины не нужно.
	QueryExecModeCacheStatement = "cache_statement"
	// QueryExecModeCacheDescribe кеширует описание запроса, но не готовит его на сервере.
	QueryExecModeCacheDescribe = "cache_describe"
	// QueryExecModeDescribeExec описывает запрос при каждом выполнении, ничего не кешируя.
	QueryExecModeDescribeExec = "describe_exec"
	// QueryExecModeExec отправляет запрос расширенным протоколом без подготовки.
	QueryExecModeExec = "exec"
	// QueryExecModeSimpleProtocol — простой протокол: аргументы инлайнятся в текст запроса,
	// именованные prepared statements не создаются вовсе. Самый медленный режим и без
	// бинарного протокола, зато не требует ничего от соединения.
	QueryExecModeSimpleProtocol = "simple_protocol"

	defaultPort           = 5432
	defaultMaxConns       = 20
	defaultMinConns       = 5
	defaultConnLifetime   = 30 * time.Minute
	defaultConnIdleTime   = 2 * time.Minute
	defaultSlowQueryLimit = 200 * time.Millisecond
)

// TLSConfig содержит настройки TLS/mTLS подключения к PostgreSQL.
// Байтовые поля приоритетнее файловых — тот же контракт, что в http/server.
type TLSConfig struct {
	CertFile string `env:"DB_TLS_CERT_FILE" yaml:"cert_file"`
	KeyFile  string `env:"DB_TLS_KEY_FILE"  yaml:"key_file"`
	CAFile   string `env:"DB_TLS_CA_FILE"   yaml:"ca_file"`

	ServerName string `env:"DB_TLS_SERVER_NAME" yaml:"server_name"`

	// Сертификаты в виде байтовых массивов (приоритетнее, чем файлы)
	CertData []byte `env:"DB_TLS_CERT_DATA" yaml:"cert_data"`
	KeyData  []byte `env:"DB_TLS_KEY_DATA"  yaml:"key_data"`
	CAData   []byte `env:"DB_TLS_CA_DATA"   yaml:"ca_data"`

	Enabled       bool `env:"DB_TLS_ENABLED"     yaml:"enabled"        env-default:"false"`
	Base64Encoded bool `env:"DB_TLS_BASE64"      yaml:"base64_encoded" env-default:"false"`
	//nolint:gosec // осознанная опция для self-signed сертификатов в dev-контурах
	InsecureSkipVerify bool `env:"DB_TLS_INSECURE_SKIP_VERIFY" yaml:"insecure_skip_verify" env-default:"false"`
}

// Config конфигурация подключения к PostgreSQL.
type Config struct {
	// DSN — готовая строка подключения. Если задана, поля Host/Port/User/Password/Database
	// игнорируются. Удобно для managed-баз, отдающих строку целиком.
	DSN string `env:"DB_DSN" yaml:"dsn"`

	// Host допускает список хостов через запятую: "pg1,pg2:5433,pg3".
	// Вместе с TargetSessionAttrs это даёт failover средствами самого драйвера.
	Host string `env:"DB_HOST" yaml:"host" env-default:"localhost"`

	User     string `env:"DB_USER"     yaml:"user"     required:"true"`
	Password string `env:"DB_PASSWORD" yaml:"password" required:"true"`
	Database string `env:"DB_DATABASE" yaml:"database" required:"true"`
	SSLMode  string `env:"DB_SSLMODE"  yaml:"ssl_mode" env-default:"disable"`

	// TargetSessionAttrs: any | read-write | read-only | primary | standby | prefer-standby.
	// read-write заставляет драйвер перебирать хосты до живого мастера.
	TargetSessionAttrs string `env:"DB_TARGET_SESSION_ATTRS" yaml:"target_session_attrs"`

	// ReplicaHosts включает разделение чтения и записи: запросы, помеченные
	// postgres.ReadOnly(ctx), уходят в отдельный пул к репликам.
	ReplicaHosts string `env:"DB_REPLICA_HOSTS" yaml:"replica_hosts"`

	// QueryExecMode: cache_statement | cache_describe | describe_exec | exec | simple_protocol.
	QueryExecMode string `env:"DB_QUERY_EXEC_MODE" yaml:"query_exec_mode" env-default:"cache_statement"`

	ApplicationName string `env:"DB_APPLICATION_NAME" yaml:"application_name"`

	TLS TLSConfig `yaml:"tls"`

	MaxConnLifetime time.Duration `env:"DB_MAX_CONN_LIFETIME"  yaml:"max_conn_lifetime"  env-default:"30m"`
	MaxConnIdleTime time.Duration `env:"DB_MAX_CONN_IDLE_TIME" yaml:"max_conn_idle_time" env-default:"2m"`
	ConnectTimeout  time.Duration `env:"DB_CONNECT_TIMEOUT"    yaml:"connect_timeout"    env-default:"5s"`

	// Серверные таймауты. В отличие от отмены по context они гарантируют, что PostgreSQL
	// действительно прекратит выполнение: отмена по context — это гонка, тяжёлый запрос
	// может дожигать CPU уже никому не нужным.
	StatementTimeout            time.Duration `env:"DB_STATEMENT_TIMEOUT"              yaml:"statement_timeout"`
	LockTimeout                 time.Duration `env:"DB_LOCK_TIMEOUT"                   yaml:"lock_timeout"`
	IdleInTxSessionTimeout      time.Duration `env:"DB_IDLE_IN_TX_SESSION_TIMEOUT"     yaml:"idle_in_tx_session_timeout"`
	SlowQueryThreshold          time.Duration `env:"DB_SLOW_QUERY_THRESHOLD"           yaml:"slow_query_threshold" env-default:"200ms"`
	HealthcheckAcquireThreshold time.Duration `env:"DB_HEALTHCHECK_ACQUIRE_THRESHOLD"  yaml:"healthcheck_acquire_threshold" env-default:"1s"`

	Port     int   `env:"DB_PORT"      yaml:"port"      env-default:"5432"`
	MaxConns int32 `env:"DB_MAX_CONNS" yaml:"max_conns" env-default:"20"`
	MinConns int32 `env:"DB_MIN_CONNS" yaml:"min_conns" env-default:"5"`

	// Инструментация выключена по умолчанию: включается переменными окружения
	// без пересборки либо опциями WithTracing()/WithMetrics().
	TracingEnabled  bool `env:"DB_TRACING_ENABLED"   yaml:"tracing_enabled"   env-default:"false"`
	MetricsEnabled  bool `env:"DB_METRICS_ENABLED"   yaml:"metrics_enabled"   env-default:"false"`
	QueryLogEnabled bool `env:"DB_QUERY_LOG_ENABLED" yaml:"query_log_enabled" env-default:"false"`
	SlowQueryLog    bool `env:"DB_SLOW_QUERY_LOG"    yaml:"slow_query_log"    env-default:"false"`
}

// DefaultConfig возвращает конфигурацию с безопасными значениями по умолчанию.
func DefaultConfig() Config {
	return Config{
		Host:               "localhost",
		Port:               defaultPort,
		SSLMode:            "disable",
		QueryExecMode:      QueryExecModeCacheStatement,
		MaxConns:           defaultMaxConns,
		MinConns:           defaultMinConns,
		MaxConnLifetime:    defaultConnLifetime,
		MaxConnIdleTime:    defaultConnIdleTime,
		ConnectTimeout:     5 * time.Second, //nolint:mnd
		SlowQueryThreshold: defaultSlowQueryLimit,

		HealthcheckAcquireThreshold: time.Second,
	}
}

// Validate проверяет конфигурацию до попытки подключения.
func (c Config) Validate() error {
	if c.DSN == "" {
		if c.User == "" {
			return fmt.Errorf("%w: user is required", ErrInvalidConfig)
		}

		if c.Database == "" {
			return fmt.Errorf("%w: database is required", ErrInvalidConfig)
		}
	}

	if _, err := parseQueryExecMode(c.QueryExecMode); err != nil {
		return err
	}

	if c.MaxConns > 0 && c.MinConns > c.MaxConns {
		return fmt.Errorf("%w: min_conns (%d) exceeds max_conns (%d)", ErrInvalidConfig, c.MinConns, c.MaxConns)
	}

	return nil
}

// connString собирает строку подключения. В отличие от конкатенации через fmt.Sprintf
// url.URL экранирует пароль, поэтому спецсимволы в нём больше не ломают подключение.
func (c Config) connString() string {
	if c.DSN != "" {
		return c.DSN
	}

	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   joinHosts(c.Host, c.Port),
		Path:   "/" + c.Database,
	}

	query := url.Values{}
	if c.SSLMode != "" {
		query.Set("sslmode", c.SSLMode)
	}

	if c.TargetSessionAttrs != "" {
		query.Set("target_session_attrs", c.TargetSessionAttrs)
	}

	if c.ApplicationName != "" {
		query.Set("application_name", c.ApplicationName)
	}

	if c.ConnectTimeout > 0 {
		query.Set("connect_timeout", strconv.Itoa(int(c.ConnectTimeout.Seconds())))
	}

	dsn.RawQuery = query.Encode()

	return dsn.String()
}

// replicaConnString собирает строку подключения к репликам.
func (c Config) replicaConnString() string {
	replica := c
	replica.DSN = ""
	replica.Host = c.ReplicaHosts

	// Реплики обслуживают только чтение — просим драйвер выбирать standby.
	if replica.TargetSessionAttrs == "" || replica.TargetSessionAttrs == "read-write" {
		replica.TargetSessionAttrs = "prefer-standby"
	}

	return replica.connString()
}

// runtimeParams возвращает серверные параметры сессии, выставляемые при подключении.
func (c Config) runtimeParams() map[string]string {
	params := make(map[string]string, 3)

	if c.StatementTimeout > 0 {
		params["statement_timeout"] = milliseconds(c.StatementTimeout)
	}

	if c.LockTimeout > 0 {
		params["lock_timeout"] = milliseconds(c.LockTimeout)
	}

	if c.IdleInTxSessionTimeout > 0 {
		params["idle_in_transaction_session_timeout"] = milliseconds(c.IdleInTxSessionTimeout)
	}

	return params
}

// instrumented сообщает, нужен ли трейсер вообще. Если нет — ConnConfig.Tracer остаётся nil
// и накладные расходы инструментации строго нулевые.
func (c Config) instrumented() bool {
	return c.TracingEnabled || c.MetricsEnabled || c.QueryLogEnabled || c.SlowQueryLog
}

// tlsConfig собирает *tls.Config из файлов или байтовых полей.
func (t TLSConfig) tlsConfig() (*tls.Config, error) {
	if !t.Enabled {
		return nil, nil //nolint:nilnil // отсутствие TLS — валидное состояние, не ошибка
	}

	//nolint:gosec // InsecureSkipVerify управляется конфигурацией осознанно
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         t.ServerName,
		InsecureSkipVerify: t.InsecureSkipVerify,
	}

	certPEM, err := t.read(t.CertData, t.CertFile)
	if err != nil {
		return nil, fmt.Errorf("read tls cert: %w", err)
	}

	keyPEM, err := t.read(t.KeyData, t.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("read tls key: %w", err)
	}

	if len(certPEM) > 0 && len(keyPEM) > 0 {
		pair, certErr := tls.X509KeyPair(certPEM, keyPEM)
		if certErr != nil {
			return nil, fmt.Errorf("build x509 key pair: %w", certErr)
		}

		cfg.Certificates = []tls.Certificate{pair}
	}

	caPEM, err := t.read(t.CAData, t.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read tls ca: %w", err)
	}

	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("%w: ca certificate is not valid PEM", ErrInvalidConfig)
		}

		cfg.RootCAs = pool
	}

	return cfg, nil
}

// read возвращает PEM из байтового поля (приоритет) либо из файла.
func (t TLSConfig) read(data []byte, path string) ([]byte, error) {
	if len(data) > 0 {
		if !t.Base64Encoded {
			return data, nil
		}

		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("decode base64: %w", err)
		}

		return decoded, nil
	}

	if path == "" {
		return nil, nil
	}

	content, err := os.ReadFile(path) //nolint:gosec // путь задаётся конфигурацией сервиса
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}

	return content, nil
}

// parseQueryExecMode переводит строковое значение конфига в режим pgx.
func parseQueryExecMode(mode string) (pgx.QueryExecMode, error) {
	switch mode {
	case "", QueryExecModeCacheStatement:
		return pgx.QueryExecModeCacheStatement, nil
	case QueryExecModeCacheDescribe:
		return pgx.QueryExecModeCacheDescribe, nil
	case QueryExecModeDescribeExec:
		return pgx.QueryExecModeDescribeExec, nil
	case QueryExecModeExec:
		return pgx.QueryExecModeExec, nil
	case QueryExecModeSimpleProtocol:
		return pgx.QueryExecModeSimpleProtocol, nil
	default:
		return 0, fmt.Errorf("%w: unknown query_exec_mode %q", ErrInvalidConfig, mode)
	}
}

// joinHosts приводит список хостов к виду "h1:5432,h2:5433", добавляя порт по умолчанию тем,
// у кого он не указан явно.
func joinHosts(hosts string, port int) string {
	if port <= 0 {
		port = defaultPort
	}

	parts := strings.Split(hosts, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		host := strings.TrimSpace(part)
		if host == "" {
			continue
		}

		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(host, strconv.Itoa(port))
		}

		out = append(out, host)
	}

	if len(out) == 0 {
		return net.JoinHostPort("localhost", strconv.Itoa(port))
	}

	return strings.Join(out, ",")
}

// milliseconds форматирует длительность для серверных параметров PostgreSQL.
func milliseconds(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}
