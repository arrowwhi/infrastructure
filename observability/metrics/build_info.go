package metrics

import (
	"context"
	"fmt"
	goruntime "runtime"
	"runtime/debug"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// BuildInfoMetric — имя метрики с информацией о сборке. В Prometheus: app_build_info.
const BuildInfoMetric = "app.build.info"

// registerBuildInfo публикует gauge app.build.info со значением 1 и метками версии,
// окружения, версии Go и git-ревизии. Классический приём для дашбордов и алертов:
// видно, какая версия где раскатана, а join по меткам позволяет подписать графики версией.
func registerBuildInfo(mp metric.MeterProvider, serviceName, version, environment string) error {
	attrs := metric.WithAttributes(buildInfoAttributes(serviceName, version, environment)...)

	_, err := mp.Meter(serviceName).Int64ObservableGauge(
		BuildInfoMetric,
		metric.WithDescription("Build information: constant 1 labelled with version, environment, Go version and VCS revision"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(1, attrs)

			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to register build info metric: %w", err)
	}

	return nil
}

func buildInfoAttributes(serviceName, version, environment string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("service", serviceName),
		attribute.String("version", version),
		attribute.String("environment", environment),
		attribute.String("go_version", goruntime.Version()),
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				attrs = append(attrs, attribute.String("revision", setting.Value))
			case "vcs.modified":
				attrs = append(attrs, attribute.String("dirty", setting.Value))
			}
		}
	}

	return attrs
}
