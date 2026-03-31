package logsetup

import (
	"context"
	"log/slog"
)

type contextKey string

const (
	operationIDContextKey contextKey = "operation_id"
	sessionIDContextKey   contextKey = "session_id"
)

func WithOperationID(ctx context.Context, operationID string) context.Context {
	if operationID == "" {
		return ctx
	}
	return context.WithValue(ctx, operationIDContextKey, operationID)
}

func OperationIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	operationID, ok := ctx.Value(operationIDContextKey).(string)
	if !ok || operationID == "" {
		return "", false
	}
	return operationID, true
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDContextKey, sessionID)
}

func SessionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	sessionID, ok := ctx.Value(sessionIDContextKey).(string)
	if !ok || sessionID == "" {
		return "", false
	}
	return sessionID, true
}

// Cosider https://github.com/PumpkinSeed/slog-context instead
type contextHandler struct {
	handler slog.Handler
}

func newContextHandler(handler slog.Handler) slog.Handler {
	return &contextHandler{handler: handler}
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if operationID, ok := OperationIDFromContext(ctx); ok {
		record.AddAttrs(slog.String("operation_id", operationID))
	}
	if sessionID, ok := SessionIDFromContext(ctx); ok {
		record.AddAttrs(slog.String("session_id", sessionID))
	}
	return h.handler.Handle(ctx, record)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{handler: h.handler.WithGroup(name)}
}
