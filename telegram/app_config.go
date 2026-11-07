package telegram

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"

	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"
)

// AppConfig — общая часть конфигурации любого сервиса. Встраивается в конфиг сервиса:
//
//	type Config struct {
//	    App      telegram.AppConfig `env-prefix:""`
//	    Telegram telegram.Config
//	    Postgres postgres.Config
//	}
type AppConfig struct {
	ServiceName string `yaml:"service_name" env:"SERVICE_NAME" env-required:"true"`
	Version     string `yaml:"version" env:"SERVICE_VERSION" env-default:"dev"`
	Environment string `yaml:"environment" env:"ENVIRONMENT" env-default:"local"`

	Logger  logger.Config  `yaml:"logger" env-prefix:"LOG_"`
	Metrics metrics.Config `yaml:"metrics" env-prefix:"METRICS_"`
	Tracing tracing.Config `yaml:"tracing"`
	Admin   AdminConfig    `yaml:"admin"`

	// ShutdownTimeout — сколько ждать остановки компонентов и сброса телеметрии.
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SHUTDOWN_TIMEOUT" env-default:"30s"`
}

// AdminConfig — служебный порт: /metrics, /healthz, /ready и (опционально) /debug/pprof.
// Его не публикуют наружу, на него смотрят Prometheus и пробы Kubernetes.
type AdminConfig struct {
	Host         string `yaml:"host" env:"ADMIN_HOST" env-default:"0.0.0.0"`
	Port         string `yaml:"port" env:"ADMIN_PORT" env-default:"9090"`
	Enabled      bool   `yaml:"enabled" env:"ADMIN_ENABLED" env-default:"true"`
	PprofEnabled bool   `yaml:"pprof_enabled" env:"ADMIN_PPROF_ENABLED" env-default:"false"`
}

// Addr возвращает адрес служебного сервера.
func (c AdminConfig) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
}

// LoadConfig заполняет cfg из переменных окружения (теги env, env-default, env-prefix).
// Если в рабочем каталоге есть .env, он читается первым; переменные окружения его
// перекрывают. Отсутствие обязательного поля (env-required) — ошибка.
func LoadConfig(cfg any) error {
	if _, err := os.Stat(".env"); err == nil {
		if err = cleanenv.ReadConfig(".env", cfg); err != nil {
			return fmt.Errorf("read .env: %w", err)
		}

		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat .env: %w", err)
	}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		return fmt.Errorf("read env: %w", err)
	}

	return nil
}
