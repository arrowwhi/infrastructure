package metrics

import (
	"context"
	"errors"
	"io"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// DurationBuckets — границы гистограмм длительности в секундах. Подходят и для
// быстрых RPC (единицы миллисекунд), и для медленных внешних вызовов (до 10 с).
var DurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type grpcMetrics struct {
	requestCounter  metric.Int64Counter
	requestDuration metric.Float64Histogram
	requestSize     metric.Int64Histogram
	responseSize    metric.Int64Histogram
	activeRequests  metric.Int64UpDownCounter
	streamMessages  metric.Int64Counter
}

// newGRPCMetrics создаёт инструменты с префиксом side: "grpc.server" или "grpc.client".
func newGRPCMetrics(serviceName, side string) *grpcMetrics {
	meter := otel.Meter(serviceName)

	requestCounter, _ := meter.Int64Counter(
		side+".request.count",
		metric.WithDescription("Total number of gRPC requests"),
		metric.WithUnit("{request}"),
	)

	requestDuration, _ := meter.Float64Histogram(
		side+".request.duration",
		metric.WithDescription("gRPC request duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(DurationBuckets...),
	)

	requestSize, _ := meter.Int64Histogram(
		side+".request.size",
		metric.WithDescription("gRPC request message size"),
		metric.WithUnit("By"),
	)

	responseSize, _ := meter.Int64Histogram(
		side+".response.size",
		metric.WithDescription("gRPC response message size"),
		metric.WithUnit("By"),
	)

	activeRequests, _ := meter.Int64UpDownCounter(
		side+".active_requests",
		metric.WithDescription("Number of active gRPC requests"),
		metric.WithUnit("{request}"),
	)

	streamMessages, _ := meter.Int64Counter(
		side+".stream.messages",
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

// recordCall записывает итог вызова: счётчик, длительность и размеры сообщений.
func (m *grpcMetrics) recordCall(ctx context.Context, method string, start time.Time, err error, req, resp any) {
	attrs := metric.WithAttributes(
		attribute.String("grpc.method", method),
		attribute.String("grpc.status", status.Code(err).String()),
	)

	m.requestCounter.Add(ctx, 1, attrs)
	m.requestDuration.Record(ctx, time.Since(start).Seconds(), attrs)

	if sizer, ok := req.(interface{ Size() int }); ok {
		m.requestSize.Record(ctx, int64(sizer.Size()), attrs)
	}

	if sizer, ok := resp.(interface{ Size() int }); ok && err == nil {
		m.responseSize.Record(ctx, int64(sizer.Size()), attrs)
	}
}

// UnaryServerInterceptor возвращает unary interceptor для сбора серверных метрик.
func UnaryServerInterceptor(serviceName string) grpc.UnaryServerInterceptor {
	m := newGRPCMetrics(serviceName, "grpc.server")

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()

		active := metric.WithAttributes(attribute.String("grpc.method", info.FullMethod))
		m.activeRequests.Add(ctx, 1, active)

		defer m.activeRequests.Add(ctx, -1, active)

		resp, err := handler(ctx, req)
		m.recordCall(ctx, info.FullMethod, start, err, req, resp)

		return resp, err
	}
}

// StreamServerInterceptor возвращает stream interceptor для сбора серверных метрик.
func StreamServerInterceptor(serviceName string) grpc.StreamServerInterceptor {
	m := newGRPCMetrics(serviceName, "grpc.server")

	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()
		ctx := ss.Context()

		active := metric.WithAttributes(attribute.String("grpc.method", info.FullMethod))
		m.activeRequests.Add(ctx, 1, active)

		defer m.activeRequests.Add(ctx, -1, active)

		err := handler(srv, &metricsServerStream{ServerStream: ss, metrics: m, method: info.FullMethod})
		m.recordCall(ctx, info.FullMethod, start, err, nil, nil)

		return err
	}
}

// UnaryClientInterceptor возвращает unary interceptor для сбора клиентских метрик
// (grpc.client.*). Стоит ставить его снаружи ретраев: тогда длительность и статус
// относятся к логическому вызову, а не к отдельной попытке.
func UnaryClientInterceptor(serviceName string) grpc.UnaryClientInterceptor {
	m := newGRPCMetrics(serviceName, "grpc.client")

	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		start := time.Now()

		active := metric.WithAttributes(attribute.String("grpc.method", method))
		m.activeRequests.Add(ctx, 1, active)

		defer m.activeRequests.Add(ctx, -1, active)

		err := invoker(ctx, method, req, reply, cc, opts...)
		m.recordCall(ctx, method, start, err, req, reply)

		return err
	}
}

// StreamClientInterceptor возвращает stream interceptor для сбора клиентских метрик.
// Длительность считается от открытия стрима до получения его итогового статуса.
func StreamClientInterceptor(serviceName string) grpc.StreamClientInterceptor {
	m := newGRPCMetrics(serviceName, "grpc.client")

	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		start := time.Now()

		cs, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			m.recordCall(ctx, method, start, err, nil, nil)

			return nil, err
		}

		return &metricsClientStream{ClientStream: cs, metrics: m, method: method, start: start, ctx: ctx}, nil
	}
}

// metricsServerStream оборачивает grpc.ServerStream для подсчета сообщений.
type metricsServerStream struct {
	grpc.ServerStream
	metrics *grpcMetrics
	method  string
}

func (s *metricsServerStream) SendMsg(m any) error {
	err := s.ServerStream.SendMsg(m)
	if err == nil {
		s.metrics.countMessage(s.Context(), s.method, "sent", m, s.metrics.responseSize)
	}

	return err
}

func (s *metricsServerStream) RecvMsg(m any) error {
	err := s.ServerStream.RecvMsg(m)
	if err == nil {
		s.metrics.countMessage(s.Context(), s.method, "received", m, s.metrics.requestSize)
	}

	return err
}

// metricsClientStream оборачивает grpc.ClientStream: считает сообщения и пишет итог
// вызова, когда RecvMsg вернул ошибку (io.EOF — штатное завершение).
type metricsClientStream struct {
	grpc.ClientStream
	ctx     context.Context //nolint:containedctx // нужен для записи итоговой метрики
	metrics *grpcMetrics
	start   time.Time
	method  string
	done    bool
}

func (s *metricsClientStream) SendMsg(m any) error {
	err := s.ClientStream.SendMsg(m)
	if err == nil {
		s.metrics.countMessage(s.ctx, s.method, "sent", m, s.metrics.requestSize)
	}

	return err
}

func (s *metricsClientStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err == nil {
		s.metrics.countMessage(s.ctx, s.method, "received", m, s.metrics.responseSize)

		return nil
	}

	if !s.done {
		s.done = true

		finalErr := err
		if isEOF(err) {
			finalErr = nil
		}

		s.metrics.recordCall(s.ctx, s.method, s.start, finalErr, nil, nil)
	}

	return err
}

func (m *grpcMetrics) countMessage(ctx context.Context, method, direction string, msg any, size metric.Int64Histogram) {
	m.streamMessages.Add(ctx, 1, metric.WithAttributes(
		attribute.String("grpc.method", method),
		attribute.String("direction", direction),
	))

	if sizer, ok := msg.(interface{ Size() int }); ok {
		size.Record(ctx, int64(sizer.Size()), metric.WithAttributes(attribute.String("grpc.method", method)))
	}
}

func isEOF(err error) bool {
	return errors.Is(err, io.EOF)
}
