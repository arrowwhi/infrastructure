package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// compositeTracer раскладывает вызовы pgx по нескольким трейсерам.
//
// Существует потому, что pgx.ConnConfig.Tracer — единственное поле: повесить туда
// одновременно трейсинг, метрики и логирование иначе невозможно.
//
// Контракт pgx требует аккуратности в двух местах:
//   - TraceXxxStart возвращает context.Context, и его надо прокидывать по цепочке,
//     иначе вложенные трейсеры не увидят данных, положенных предыдущими;
//   - TraceXxxEnd вызывается в обратном порядке, чтобы вложенность была симметричной.
//
// Все интерфейсы, кроме QueryTracer, у pgx опциональные, поэтому каждый вложенный
// трейсер проверяется приведением типа.
type compositeTracer struct {
	tracers []pgx.QueryTracer
}

var (
	_ pgx.QueryTracer    = (*compositeTracer)(nil)
	_ pgx.BatchTracer    = (*compositeTracer)(nil)
	_ pgx.CopyFromTracer = (*compositeTracer)(nil)
	_ pgx.PrepareTracer  = (*compositeTracer)(nil)
	_ pgx.ConnectTracer  = (*compositeTracer)(nil)
)

// newCompositeTracer собирает цепочку, отбрасывая пустые элементы.
// Возвращает nil, если инструментировать нечем: тогда ConnConfig.Tracer останется nil
// и pgx не будет тратить ни такта на вызовы трейсера.
func newCompositeTracer(tracers ...pgx.QueryTracer) pgx.QueryTracer {
	filtered := make([]pgx.QueryTracer, 0, len(tracers))

	for _, tracer := range tracers {
		if tracer != nil {
			filtered = append(filtered, tracer)
		}
	}

	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return &compositeTracer{tracers: filtered}
	}
}

func (c *compositeTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, tracer := range c.tracers {
		ctx = tracer.TraceQueryStart(ctx, conn, data)
	}

	return ctx
}

func (c *compositeTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for i := len(c.tracers) - 1; i >= 0; i-- {
		c.tracers[i].TraceQueryEnd(ctx, conn, data)
	}
}

func (c *compositeTracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	for _, tracer := range c.tracers {
		if t, ok := tracer.(pgx.BatchTracer); ok {
			ctx = t.TraceBatchStart(ctx, conn, data)
		}
	}

	return ctx
}

func (c *compositeTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	for i := len(c.tracers) - 1; i >= 0; i-- {
		if t, ok := c.tracers[i].(pgx.BatchTracer); ok {
			t.TraceBatchQuery(ctx, conn, data)
		}
	}
}

func (c *compositeTracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	for i := len(c.tracers) - 1; i >= 0; i-- {
		if t, ok := c.tracers[i].(pgx.BatchTracer); ok {
			t.TraceBatchEnd(ctx, conn, data)
		}
	}
}

func (c *compositeTracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	for _, tracer := range c.tracers {
		if t, ok := tracer.(pgx.CopyFromTracer); ok {
			ctx = t.TraceCopyFromStart(ctx, conn, data)
		}
	}

	return ctx
}

func (c *compositeTracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	for i := len(c.tracers) - 1; i >= 0; i-- {
		if t, ok := c.tracers[i].(pgx.CopyFromTracer); ok {
			t.TraceCopyFromEnd(ctx, conn, data)
		}
	}
}

func (c *compositeTracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	for _, tracer := range c.tracers {
		if t, ok := tracer.(pgx.PrepareTracer); ok {
			ctx = t.TracePrepareStart(ctx, conn, data)
		}
	}

	return ctx
}

func (c *compositeTracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	for i := len(c.tracers) - 1; i >= 0; i-- {
		if t, ok := c.tracers[i].(pgx.PrepareTracer); ok {
			t.TracePrepareEnd(ctx, conn, data)
		}
	}
}

func (c *compositeTracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	for _, tracer := range c.tracers {
		if t, ok := tracer.(pgx.ConnectTracer); ok {
			ctx = t.TraceConnectStart(ctx, data)
		}
	}

	return ctx
}

func (c *compositeTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	for i := len(c.tracers) - 1; i >= 0; i-- {
		if t, ok := c.tracers[i].(pgx.ConnectTracer); ok {
			t.TraceConnectEnd(ctx, data)
		}
	}
}
