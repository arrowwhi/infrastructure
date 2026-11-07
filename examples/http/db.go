package main

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/render"
	"go.opentelemetry.io/otel/metric"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/examples/http/config"
	"github.com/arrowwhi/infrastructure/observability/logger"
)

// schema создаётся на старте, чтобы пример был самодостаточным.
//
// Пользователь 123 засеян намеренно: именно этот идентификатор дёргают
// `make test-endpoints` и `make load-test`, поэтому нагрузка сразу идёт в базу.
const schema = `
CREATE TABLE IF NOT EXISTS users (
	id         bigint PRIMARY KEY,
	name       text NOT NULL,
	email      text NOT NULL UNIQUE,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS orders (
	id         bigserial PRIMARY KEY,
	user_id    bigint NOT NULL REFERENCES users (id),
	product_id int NOT NULL,
	amount     numeric(12, 2) NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO users (id, name, email) VALUES
	(123, 'John Doe', 'john@example.com'),
	(456, 'Jane Roe', 'jane@example.com')
ON CONFLICT (id) DO NOTHING;`

// dbUser — строка таблицы users. Колонки сопоставляются с полями по имени,
// переопределить имя можно тегом `db`.
type dbUser struct {
	CreatedAt time.Time `db:"created_at"`
	Name      string    `db:"name"`
	Email     string    `db:"email"`
	ID        int64     `db:"id"`
}

// connectPostgres поднимает пул с включёнными трейсами и метриками.
//
// Возвращает nil, если база недоступна: пример должен запускаться и без PostgreSQL,
// чтобы make run-server работал сам по себе. Тогда ручки отдают заглушки.
func connectPostgres(ctx context.Context, cfg config.Config, meterProvider metric.MeterProvider) *postgres.Pool {
	pool, err := postgres.New(ctx, cfg.Postgres,
		postgres.WithServiceName(cfg.ServiceName),
		postgres.WithMeterProvider(meterProvider),
	)
	if err != nil {
		logger.Warn(ctx).
			Err(err).
			Msg("PostgreSQL is unavailable, handlers will fall back to stub data (run `make obs-up` to start it)")

		return nil
	}

	if _, err = pool.Exec(postgres.WithQueryName(ctx, "schema.init"), schema); err != nil {
		logger.Error(ctx).Err(err).Msg("Failed to initialize database schema")
		pool.Close()

		return nil
	}

	logger.Info(ctx).Msg("PostgreSQL connected, handlers will query the database")

	return pool
}

// renderDBError переводит ошибки PostgreSQL в осмысленные коды ответа.
func renderDBError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case postgres.IsNoRows(err):
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "not found"})
	case postgres.IsUniqueViolation(err):
		render.Status(r, http.StatusConflict)
		render.JSON(w, r, map[string]string{"error": "already exists"})
	case postgres.IsForeignKeyViolation(err):
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "referenced entity does not exist"})
	case postgres.IsQueryCanceled(err):
		render.Status(r, http.StatusGatewayTimeout)
		render.JSON(w, r, map[string]string{"error": "query timed out"})
	default:
		logger.Error(r.Context()).Err(err).Msg("database request failed")
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "internal error"})
	}
}
