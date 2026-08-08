package logsetup

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"go.opentelemetry.io/otel/trace"
)

func TestContextHandlerAddsTraceAndSpanIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
	}))

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, "trace_id=01020300000000000000000000000000") {
		t.Fatalf("expected trace_id in output, got %q", output)
	}
	if !strings.Contains(output, "span_id=0405060000000000") {
		t.Fatalf("expected span_id in output, got %q", output)
	}
}

func TestContextHandlerAddsSessionID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))
	ctx := WithSessionID(context.Background(), "sess-123")

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, "session_id=sess-123") {
		t.Fatalf("expected session_id in output, got %q", output)
	}
}

func TestContextHandlerAddsUserID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))
	ctx := WithUserID(context.Background(), "user-123")

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, "user_id=user-123") {
		t.Fatalf("expected user_id in output, got %q", output)
	}
}

func TestContextHandlerFallsBackToClerkUserID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))
	ctx := clerk.ContextWithSessionClaims(context.Background(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{
			Subject: "clerk-user-123",
		},
	})

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, "user_id=clerk-user-123") {
		t.Fatalf("expected clerk user_id in output, got %q", output)
	}
}

func TestContextHandlerPrefersExplicitUserID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))
	ctx := clerk.ContextWithSessionClaims(context.Background(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{
			Subject: "clerk-user-123",
		},
	})
	ctx = WithUserID(ctx, "explicit-user-123")

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, "user_id=explicit-user-123") {
		t.Fatalf("expected explicit user_id in output, got %q", output)
	}
	if strings.Contains(output, "clerk-user-123") {
		t.Fatalf("did not expect clerk user_id in output, got %q", output)
	}
}

func TestContextHandlerAddsBothIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
	}))
	ctx = WithSessionID(ctx, "sess-123")

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, "trace_id=01020300000000000000000000000000") {
		t.Fatalf("expected trace_id in output, got %q", output)
	}
	if !strings.Contains(output, "session_id=sess-123") {
		t.Fatalf("expected session_id in output, got %q", output)
	}
}

func TestContextHandlerSkipsMissingTraceID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.InfoContext(context.Background(), "hello")

	output := buf.String()
	if strings.Contains(output, "trace_id=") {
		t.Fatalf("did not expect trace_id in output, got %q", output)
	}
}

func TestContextHandlerSkipsMissingSessionID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.InfoContext(context.Background(), "hello")

	output := buf.String()
	if strings.Contains(output, "session_id=") {
		t.Fatalf("did not expect session_id in output, got %q", output)
	}
}

func TestContextHandlerSkipsMissingUserID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.InfoContext(context.Background(), "hello")

	output := buf.String()
	if strings.Contains(output, "user_id=") {
		t.Fatalf("did not expect user_id in output, got %q", output)
	}
}
