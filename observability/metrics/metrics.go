// Package metrics provides OpenTelemetry metrics integration with support for Prometheus, OTLP gRPC, and OTLP HTTP exporters.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// prometheusExporter хранит глобальный Prometheus экспортер
var prometheusExporter *prometheus.Exporter

// InitMeterProvider инициализирует MeterProvider на основе конфигурации
func InitMeterProvider(ctx context.Context, cfg Config, serviceName, serviceVersion, environment string) (*metric.MeterProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Создаем resource с информацией о сервисе
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
			semconv.DeploymentEnvironmentKey.String(environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Создаем reader в зависимости от типа экспортера
	var reader metric.Reader
	switch cfg.Exporter {
	case "prometheus":
		reader, err = createPrometheusReader()
	case "otlp-grpc":
		reader, err = createOTLPGRPCReader(ctx, cfg)
	case "otlp-http":
		reader, err = createOTLPHTTPReader(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown exporter type: %s", cfg.Exporter)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}

	// Создаем MeterProvider
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(reader),
	)

	// Устанавливаем глобальный MeterProvider
	otel.SetMeterProvider(mp)

	return mp, nil
}

func createPrometheusReader() (metric.Reader, error) {
	exporter, err := prometheus.New(
		prometheus.WithRegisterer(promclient.DefaultRegisterer),
	)
	if err != nil {
		return nil, err
	}
	// Сохраняем экспортер для доступа к HTTP handler
	prometheusExporter = exporter
	return exporter, nil
}

// Handler возвращает HTTP handler для Prometheus метрик.
// Должен быть зарегистрирован в роутере на путь /metrics.
// Возвращает nil, если Prometheus экспортер не инициализирован.
func Handler() http.Handler {
	if prometheusExporter == nil {
		return nil
	}
	// Prometheus экспортер регистрируется как collector в DefaultRegistry,
	// поэтому используем стандартный promhttp.Handler()
	return promhttp.Handler()
}

func createOTLPGRPCReader(ctx context.Context, cfg Config) (metric.Reader, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
	}

	if cfg.OTLPInsecure {
		opts = append(opts,
			otlpmetricgrpc.WithTLSCredentials(insecure.NewCredentials()),
			otlpmetricgrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	}

	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return metric.NewPeriodicReader(exporter,
		metric.WithInterval(time.Duration(cfg.MetricInterval)*time.Second),
	), nil
}

func createOTLPHTTPReader(ctx context.Context, cfg Config) (metric.Reader, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(cfg.OTLPEndpoint),
	}

	if cfg.OTLPInsecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return metric.NewPeriodicReader(exporter,
		metric.WithInterval(time.Duration(cfg.MetricInterval)*time.Second),
	), nil
}
