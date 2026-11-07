package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/sync/errgroup"

	"github.com/arrowwhi/infrastructure/observability/health"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"
)

const readHeaderTimeout = 10 * time.Second

// Component — долгоживущая часть сервиса (HTTP-сервер, бот, воркер).
// Run блокируется до отмены ctx и возвращает nil при штатной остановке.
type Component interface {
	Run(ctx context.Context) error
}

// ComponentFunc позволяет передать функцию как Component.
type ComponentFunc func(ctx context.Context) error

// Run реализует Component.
func (f ComponentFunc) Run(ctx context.Context) error { return f(ctx) }

// App — собранная инфраструктура сервиса.
type App struct {
	health         *health.Registry
	tracer         *tracing.TracerProvider
	meter          *sdkmetric.MeterProvider
	admin          *http.Server
	closers        []func(context.Context) error
	cfg            AppConfig
	mu             sync.Mutex
	shutdownSignal []os.Signal
}

// NewApp инициализирует логгер, трейсинг, метрики и реестр health-проверок.
// Трейсинг и метрики регистрируются глобально (otel.Set*Provider), поэтому все
// коннекторы библиотеки подхватывают их без дополнительной настройки.
func NewApp(ctx context.Context, cfg AppConfig) (*App, error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("app: service name is required")
	}

	logger.Init(&cfg.Logger, cfg.ServiceName, cfg.Version, cfg.Environment)

	a := &App{
		cfg:            cfg,
		health:         health.New(),
		shutdownSignal: []os.Signal{os.Interrupt, syscall.SIGTERM},
	}

	tp, err := tracing.InitTracer(ctx, cfg.Tracing, cfg.ServiceName, cfg.Version, cfg.Environment)
	if err != nil {
		return nil, fmt.Errorf("app: tracing: %w", err)
	}

	a.tracer = tp

	mp, err := metrics.InitMeterProvider(ctx, cfg.Metrics, cfg.ServiceName, cfg.Version, cfg.Environment)
	if err != nil {
		_ = tp.Shutdown(ctx)

		return nil, fmt.Errorf("app: metrics: %w", err)
	}

	a.meter = mp

	if err = a.health.RegisterMetrics(otel.Meter(cfg.ServiceName)); err != nil {
		_ = a.shutdownTelemetry(ctx)

		return nil, fmt.Errorf("app: health metrics: %w", err)
	}

	if cfg.Admin.Enabled {
		a.admin = a.newAdminServer()
	}

	return a, nil
}

// Health возвращает реестр проверок, который отдаёт /ready на служебном порту.
func (a *App) Health() *health.Registry {
	return a.health
}

// Config возвращает конфигурацию приложения.
func (a *App) Config() AppConfig {
	return a.cfg
}

// OnShutdown регистрирует функцию остановки. Функции вызываются в обратном порядке
// регистрации (LIFO) после остановки всех компонентов: то, что создано последним
// и зависит от раннего (репозиторий от пула), закрывается первым.
func (a *App) OnShutdown(fn func(ctx context.Context) error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.closers = append(a.closers, fn)
}

// Run запускает служебный сервер и компоненты, ждёт SIGINT/SIGTERM, отмены ctx или
// ошибки любого компонента, затем останавливает всё в порядке: компоненты →
// функции OnShutdown (LIFO) → сброс телеметрии. Возвращает первую ошибку компонента.
func (a *App) Run(ctx context.Context, components ...Component) error {
	ctx, stop := signal.NotifyContext(ctx, a.shutdownSignal...)
	defer stop()

	g, gCtx := errgroup.WithContext(ctx)

	if a.admin != nil {
		g.Go(func() error {
			logger.Info(gCtx).Str("addr", a.admin.Addr).Msg("Starting admin server")

			if err := a.admin.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("admin server: %w", err)
			}

			return nil
		})

		g.Go(func() error {
			<-gCtx.Done()

			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(gCtx), a.cfg.ShutdownTimeout)
			defer cancel()

			return a.admin.Shutdown(shutdownCtx)
		})
	}

	for _, c := range components {
		g.Go(func() error {
			return c.Run(gCtx)
		})
	}

	logger.Info(ctx).Msg("Service started")

	runErr := g.Wait()
	if runErr != nil {
		logger.Error(ctx).Err(runErr).Msg("Component failed, shutting down")
	} else {
		logger.Info(ctx).Msg("Shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.ShutdownTimeout)
	defer cancel()

	shutdownErr := a.Shutdown(shutdownCtx)

	return errors.Join(runErr, shutdownErr)
}

// Shutdown вызывает функции OnShutdown (LIFO) и сбрасывает телеметрию.
// Run вызывает его сам; отдельно нужен, только если Run не использовался.
func (a *App) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	closers := a.closers
	a.closers = nil
	a.mu.Unlock()

	var errs []error

	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if err := a.shutdownTelemetry(ctx); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (a *App) shutdownTelemetry(ctx context.Context) error {
	var errs []error

	if a.meter != nil {
		if err := a.meter.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("metrics shutdown: %w", err))
		}
	}

	if a.tracer != nil {
		if err := a.tracer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracing shutdown: %w", err))
		}
	}

	return errors.Join(errs...)
}

// AdminHandler возвращает обработчик служебного порта (удобно для тестов и встраивания).
func (a *App) AdminHandler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /healthz", health.LivenessHandler())
	mux.Handle("GET /ready", a.health.ReadinessHandler())

	if h := metrics.Handler(); h != nil {
		mux.Handle("GET /metrics", h)
	}

	if a.cfg.Admin.PprofEnabled {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	return mux
}

func (a *App) newAdminServer() *http.Server {
	return &http.Server{
		Addr:              a.cfg.Admin.Addr(),
		Handler:           a.AdminHandler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}
}
