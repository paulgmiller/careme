package logsetup

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestContextHandlerAddsOperationID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))
	ctx := WithOperationID(context.Background(), "op-123")

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, "operation_id=op-123") {
		t.Fatalf("expected operation_id in output, got %q", output)
	}
}

func TestContextHandlerSkipsMissingOperationID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.InfoContext(context.Background(), "hello")

	output := buf.String()
	if strings.Contains(output, "operation_id=") {
		t.Fatalf("did not expect operation_id in output, got %q", output)
	}
}
