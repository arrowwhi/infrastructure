package main

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/render"
	"github.com/oapi-codegen/runtime/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/examples/http/api/openapi"
	"github.com/arrowwhi/infrastructure/http/server/middlewares"
)

type apiServer struct {
	tracer trace.Tracer
	meter  metric.Meter
	// db может быть nil: пример должен работать и без поднятой базы,
	// тогда ручки отдают заглушки вместо реальных строк.
	db                  *postgres.Pool
	userRequestCounter  metric.Int64Counter
	orderCreatedCounter metric.Int64Counter
	errorCounter        metric.Int64Counter
	processingTime      metric.Float64Histogram
}

func newAPIServer(serviceName string, db *postgres.Pool) (*apiServer, error) {
	tracer := otel.Tracer(serviceName)
	meter := otel.Meter(serviceName)

	userRequestCounter, err := meter.Int64Counter(
		"app.user.requests",
		metric.WithDescription("Number of user requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	orderCreatedCounter, err := meter.Int64Counter(
		"app.orders.created",
		metric.WithDescription("Number of orders created"),
		metric.WithUnit("{order}"),
	)
	if err != nil {
		return nil, err
	}

	errorCounter, err := meter.Int64Counter(
		"app.errors",
		metric.WithDescription("Number of errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, err
	}

	processingTime, err := meter.Float64Histogram(
		"app.processing.duration",
		metric.WithDescription("Processing duration"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	return &apiServer{
		tracer:              tracer,
		meter:               meter,
		db:                  db,
		userRequestCounter:  userRequestCounter,
		orderCreatedCounter: orderCreatedCounter,
		errorCounter:        errorCounter,
		processingTime:      processingTime,
	}, nil
}

// SimulateError implements openapi.ServerInterface.
func (s *apiServer) SimulateError(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, span := s.tracer.Start(ctx, "simulateError")
	defer span.End()

	time.Sleep(50 * time.Millisecond)

	err := &simulatedError{msg: "Something went wrong"}
	span.RecordError(err)
	span.SetStatus(codes.Error, "Failed to process request")

	s.errorCounter.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("error.type", "simulated_error"),
		),
	)

	middlewares.SetError(r, err)
	render.Status(r, http.StatusInternalServerError)
	render.JSON(w, r, map[string]string{
		"error": err.Error(),
	})
}

// CallExternalService implements openapi.ServerInterface.
func (s *apiServer) CallExternalService(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, span := s.tracer.Start(ctx, "callExternalService")
	defer span.End()

	_, httpSpan := s.tracer.Start(ctx, "http.request",
		trace.WithAttributes(
			attribute.String("http.method", "GET"),
			attribute.String("http.url", "https://api.example.com/data"),
		),
	)
	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond) //nolint:gosec
	httpSpan.SetAttributes(
		attribute.Int("http.status_code", 200),
	)
	httpSpan.End()

	render.JSON(w, r, map[string]any{
		"data": "external data",
	})
}

// CreateOrder implements openapi.ServerInterface.
func (s *apiServer) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	// Переприсваиваем ctx, иначе вложенные span'ы лягут рядом с createOrder,
	// а не внутрь него.
	ctx, parentSpan := s.tracer.Start(ctx, "createOrder")
	defer parentSpan.End()

	_, validationSpan := s.tracer.Start(ctx, "validateOrder")
	time.Sleep(50 * time.Millisecond)
	validationSpan.AddEvent("Order validated successfully")
	validationSpan.End()

	orderID := 12345

	if s.db != nil {
		id, err := s.saveOrder(ctx)
		if err != nil {
			parentSpan.RecordError(err)
			parentSpan.SetStatus(codes.Error, "Failed to create order")
			middlewares.SetError(r, err)
			renderDBError(w, r, err)

			return
		}

		orderID = int(id)
	} else {
		_, dbSpan := s.tracer.Start(ctx, "saveOrderToDB")
		time.Sleep(100 * time.Millisecond)
		dbSpan.End()
	}

	_, notifySpan := s.tracer.Start(ctx, "sendNotification",
		trace.WithAttributes(
			attribute.String("notification.type", "email"),
		),
	)
	time.Sleep(30 * time.Millisecond)
	notifySpan.AddEvent("Email sent")
	notifySpan.End()

	parentSpan.SetStatus(codes.Ok, "Order created successfully")
	parentSpan.SetAttributes(attribute.Int("order.id", orderID))

	// Идентификатор заказа в метку метрики не кладём: это неограниченное
	// множество значений, и число временных рядов росло бы с каждым заказом.
	s.orderCreatedCounter.Add(ctx, 1)

	s.processingTime.Record(ctx, float64(time.Since(start).Milliseconds()),
		metric.WithAttributes(
			attribute.String("operation", "createOrder"),
		),
	)

	status := "created"
	response := openapi.Order{
		OrderId: &orderID,
		Status:  &status,
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, response)
}

// saveOrder создаёт заказ в транзакции.
//
// Транзакция здесь показательна: в трейсе видно четыре отдельных обращения к базе —
// BEGIN, SELECT, INSERT и COMMIT, вложенные в span ручки. Репозиторный код при этом
// ничего о транзакции не знает: pool.InTx кладёт её в контекст, а postgres.Get
// и postgres.Exec достают оттуда сами.
func (s *apiServer) saveOrder(ctx context.Context) (int64, error) {
	const (
		demoUserID = 123
		productID  = 1
		amount     = 99.90
	)

	var orderID int64

	err := s.db.InTx(ctx, func(txCtx context.Context) error {
		// Проверяем существование пользователя внутри той же транзакции.
		if _, err := postgres.Get[int64](postgres.WithQueryName(txCtx, "user.exists"), s.db,
			`SELECT id FROM users WHERE id = $1 FOR UPDATE`, demoUserID); err != nil {
			return err
		}

		id, err := postgres.Get[int64](postgres.WithQueryName(txCtx, "order.create"), s.db,
			`INSERT INTO orders (user_id, product_id, amount) VALUES ($1, $2, $3) RETURNING id`,
			demoUserID, productID, amount)
		if err != nil {
			return err
		}

		orderID = id

		return nil
	})

	return orderID, err
}

// ParallelOperations implements openapi.ServerInterface.
func (s *apiServer) ParallelOperations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, parentSpan := s.tracer.Start(ctx, "parallelOperations")
	defer parentSpan.End()

	done := make(chan bool, 3)

	for i := 1; i <= 3; i++ {
		go func(taskNum int) {
			_, taskSpan := s.tracer.Start(ctx, "parallelTask",
				trace.WithAttributes(
					attribute.Int("task.number", taskNum),
				),
			)
			defer taskSpan.End()

			time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond) //nolint:gosec
			taskSpan.AddEvent("Task completed")
			done <- true
		}(i)
	}

	for range 3 {
		<-done
	}

	render.JSON(w, r, map[string]string{
		"status": "all tasks completed",
	})
}

// GetUserById implements openapi.ServerInterface.
func (s *apiServer) GetUserById(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	start := time.Now()

	s.userRequestCounter.Add(ctx, 1)

	// Контекст, возвращённый из Start, обязательно нужно переприсвоить: без этого
	// span'ы, которые создаст коннектор БД, окажутся не внутри getUserByID,
	// а рядом с ним, и разобрать трейс станет невозможно.
	ctx, span := s.tracer.Start(ctx, "getUserByID",
		trace.WithAttributes(
			attribute.String("user.id", id),
		),
	)
	defer span.End()

	name, email := "John Doe", "john@example.com"

	if s.db != nil {
		user, err := s.fetchUser(ctx, id)
		if err != nil {
			span.RecordError(err)
			middlewares.SetError(r, err)
			renderDBError(w, r, err)

			return
		}

		name, email = user.Name, user.Email
	} else {
		time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond) //nolint:gosec
	}

	span.SetAttributes(
		attribute.String("user.name", name),
		attribute.String("user.email", email),
	)

	s.processingTime.Record(ctx, float64(time.Since(start).Milliseconds()),
		metric.WithAttributes(
			attribute.String("operation", "getUserByID"),
		),
	)

	mail := types.Email(email)
	response := openapi.User{
		Id:    &id,
		Name:  &name,
		Email: &mail,
	}

	render.JSON(w, r, response)
}

// fetchUser читает пользователя из базы.
//
// Имя запроса задаётся через контекст: без него метрика получит метку
// query_name="unknown", а в лог один раз упадёт предупреждение об этом.
func (s *apiServer) fetchUser(ctx context.Context, id string) (dbUser, error) {
	userID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return dbUser{}, postgres.ErrNoRows
	}

	ctx = postgres.WithQueryName(ctx, "user.get_by_id")

	return postgres.Get[dbUser](ctx, s.db,
		`SELECT id, name, email, created_at FROM users WHERE id = $1`, userID)
}

// LegacyHealth implements openapi.ServerInterface.
func (s *apiServer) LegacyHealth(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// Healthz implements openapi.ServerInterface.
func (s *apiServer) Healthz(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]string{
		"status": "ok",
	})
}

// Readiness implements openapi.ServerInterface.
//
// Проба готовности проверяет базу через postgres.Healthcheck: он ловит не только
// недоступность, но и исчерпание пула — состояние, при котором база жива, а сервис
// принимать трафик уже не может.
func (s *apiServer) Readiness(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		render.JSON(w, r, map[string]string{"status": "ready", "database": "disabled"})

		return
	}

	if err := s.db.Healthcheck(r.Context()); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, map[string]string{"status": "not ready", "error": err.Error()})

		return
	}

	stat := s.db.Stat()
	render.JSON(w, r, map[string]any{
		"status":         "ready",
		"acquired_conns": stat.AcquiredConns(),
		"idle_conns":     stat.IdleConns(),
		"max_conns":      stat.MaxConns(),
	})
}

type simulatedError struct {
	msg string
}

func (e *simulatedError) Error() string {
	return e.msg
}
