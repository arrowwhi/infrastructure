// Package tracing provides OpenTelemetry tracing integration with support for stdout and OTLP gRPC exporters.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type TracerProvider struct {
	provider trace.TracerProvider
	shutdown func(context.Context) error
}

// InitTracer инициализирует трассировщик
func InitTracer(ctx context.Context, cfg Config, serviceName, serviceVersion, environment string) (*TracerProvider, error) {
	if !cfg.Enabled {
		return &TracerProvider{
			provider: noop.NewTracerProvider(),
			shutdown: func(context.Context) error { return nil },
		}, nil
	}

	// Создаем ресурс с метаданными сервиса
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

	// Создаем exporter в зависимости от конфигурации
	var exporter sdktrace.SpanExporter
	switch cfg.Exporter {
	case "stdout":
		exporter, err = stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout exporter: %w", err)
		}

	case "otlp-grpc":
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		}

		if cfg.OTLPInsecure {
			opts = append(opts,
				otlptracegrpc.WithInsecure(),
				otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)
		}

		exporter, err = otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP gRPC exporter: %w", err)
		}

	default:
		return nil, fmt.Errorf("unknown exporter type: %s", cfg.Exporter)
	}

	// Создаем TracerProvider с sampler
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SamplingRate)),
	)

	// Регистрируем глобально
	otel.SetTracerProvider(tp)

	// Настраиваем пропагацию контекста (W3C Trace Context + Baggage)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return &TracerProvider{
		provider: tp,
		shutdown: tp.Shutdown,
	}, nil
}

// Tracer возвращает tracer с указанным именем
func (tp *TracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return tp.provider.Tracer(name, opts...)
}

// Shutdown останавливает TracerProvider
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.shutdown != nil {
		return tp.shutdown(ctx)
	}
	return nil
}
