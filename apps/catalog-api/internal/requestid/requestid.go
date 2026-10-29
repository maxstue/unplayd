// Package requestid carries validated request IDs through contexts into logs.
package requestid

import (
	"context"
	"log/slog"
)

type contextKey struct{}

// With returns ctx carrying the request ID.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// From returns the request ID of ctx, or "" when it has none.
func From(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// LogHandler adds the request ID of the logging context as requestId.
type LogHandler struct{ slog.Handler }

func (h LogHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := From(ctx); id != "" {
		record.AddAttrs(slog.String("requestId", id))
	}
	return h.Handler.Handle(ctx, record)
}

func (h LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return LogHandler{h.Handler.WithAttrs(attrs)}
}

func (h LogHandler) WithGroup(name string) slog.Handler {
	return LogHandler{h.Handler.WithGroup(name)}
}
