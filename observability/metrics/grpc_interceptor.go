package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

type grpcMetrics struct {
	requestCounter  metric.Int64Counter
	requestDuration metric.Float64Histogram
	requestSize     metric.Int64Histogram
	responseSize    metric.Int64Histogram
	activeRequests  metric.Int64UpDownCounter
	streamMessages  metric.Int64Counter
}

func newGRPCMetrics(serviceName string) *grpcMetrics {
	meter := otel.Meter(serviceName)

	requestCounter, _ := meter.Int64Counter(
		"grpc.server.request.count",
		metric.WithDescription("Total number of gRPC requests"),
		metric.WithUnit("{request}"),
	)

	requestDuration, _ := meter.Float64Histogram(
		"grpc.server.request.duration",
		metric.WithDescription("gRPC request duration"),
		metric.WithUnit("ms"),
	)

	requestSize, _ := meter.Int64Histogram(
		"grpc.server.request.size",
		metric.WithDescription("gRPC request message size"),
		metric.WithUnit("byte"),
	)

	responseSize, _ := meter.Int64Histogram(
		"grpc.server.response.size",
		metric.WithDescription("gRPC response message size"),
		metric.WithUnit("byte"),
	)

	activeRequests, _ := meter.Int64UpDownCounter(
		"grpc.server.active_requests",
		metric.WithDescription("Number of active gRPC requests"),
		metric.WithUnit("{request}"),
	)

	streamMessages, _ := meter.Int64Counter(
		"grpc.server.stream.messages",
		metric.WithDescription("Total number of stream messages"),
		metric.WithUnit("{message}"),
	)

	return &grpcMetrics{
		requestCounter:  requestCounter,
		requestDuration: requestDuration,
		requestSize:     requestSize,
		responseSize:    responseSize,
		activeRequests:  activeRequests,
		streamMessages:  streamMessages,
	}
}

func UnaryServerInterceptor(serviceName string) grpc.UnaryServerInterceptor {
	m := newGRPCMetrics(serviceName)

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()

		// Увеличиваем счетчик активных запросов
		attrs := []attribute.KeyValue{
			attribute.String("grpc.method", info.FullMethod),
		}
		m.activeRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
		defer m.activeRequests.Add(ctx, -1, metric.WithAttributes(attrs...))

		// Вызываем handler
		resp, err := handler(ctx, req)

		// Записываем метрики
		duration := float64(time.Since(start).Milliseconds())
		grpcStatus := status.Code(err)

		finalAttrs := []attribute.KeyValue{
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("grpc.status", grpcStatus.String()),
		}

		m.requestCounter.Add(ctx, 1, metric.WithAttributes(finalAttrs...))
		m.requestDuration.Record(ctx, duration, metric.WithAttributes(finalAttrs...))

		// Размер запроса и ответа (приблизительный, используем proto.Size если нужна точность)
		if reqSizer, ok := req.(interface{ Size() int }); ok {
			m.requestSize.Record(ctx, int64(reqSizer.Size()), metric.WithAttributes(finalAttrs...))
		}
		if respSizer, ok := resp.(interface{ Size() int }); ok {
			m.responseSize.Record(ctx, int64(respSizer.Size()), metric.WithAttributes(finalAttrs...))
		}

		return resp, err
	}
}

// StreamServerInterceptor возвращает stream interceptor для сбора метрик
func StreamServerInterceptor(serviceName string) grpc.StreamServerInterceptor {
	m := newGRPCMetrics(serviceName)

	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		// Увеличиваем счетчик активных запросов
		ctx := ss.Context()
		attrs := []attribute.KeyValue{
			attribute.String("grpc.method", info.FullMethod),
			attribute.Bool("grpc.is_client_stream", info.IsClientStream),
			attribute.Bool("grpc.is_server_stream", info.IsServerStream),
		}
		m.activeRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
		defer m.activeRequests.Add(ctx, -1, metric.WithAttributes(attrs...))

		// Оборачиваем ServerStream для подсчета сообщений
		wrappedStream := &metricsServerStream{
			ServerStream: ss,
			metrics:      m,
			method:       info.FullMethod,
		}

		// Вызываем handler
		err := handler(srv, wrappedStream)

		// Записываем метрики
		duration := float64(time.Since(start).Milliseconds())
		grpcStatus := status.Code(err)

		finalAttrs := []attribute.KeyValue{
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("grpc.status", grpcStatus.String()),
			attribute.Bool("grpc.is_client_stream", info.IsClientStream),
			attribute.Bool("grpc.is_server_stream", info.IsServerStream),
		}

		m.requestCounter.Add(ctx, 1, metric.WithAttributes(finalAttrs...))
		m.requestDuration.Record(ctx, duration, metric.WithAttributes(finalAttrs...))

		return err
	}
}

// metricsServerStream оборачивает grpc.ServerStream для подсчета сообщений
type metricsServerStream struct {
	grpc.ServerStream
	metrics *grpcMetrics
	method  string
}

func (s *metricsServerStream) SendMsg(m any) error {
	err := s.ServerStream.SendMsg(m)
	if err == nil {
		attrs := []attribute.KeyValue{
			attribute.String("grpc.method", s.method),
			attribute.String("direction", "sent"),
		}
		s.metrics.streamMessages.Add(s.Context(), 1, metric.WithAttributes(attrs...))

		if sizer, ok := m.(interface{ Size() int }); ok {
			s.metrics.responseSize.Record(s.Context(), int64(sizer.Size()),
				metric.WithAttributes(attribute.String("grpc.method", s.method)))
		}
	}

	return err
}

func (s *metricsServerStream) RecvMsg(m any) error {
	err := s.ServerStream.RecvMsg(m)
	if err == nil {
		attrs := []attribute.KeyValue{
			attribute.String("grpc.method", s.method),
			attribute.String("direction", "received"),
		}
		s.metrics.streamMessages.Add(s.Context(), 1, metric.WithAttributes(attrs...))

		if sizer, ok := m.(interface{ Size() int }); ok {
			s.metrics.requestSize.Record(s.Context(), int64(sizer.Size()),
				metric.WithAttributes(attribute.String("grpc.method", s.method)))
		}
	}

	return err
}
