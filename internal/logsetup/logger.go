package logsetup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/openclosed-dev/slogan/appinsights"
)

// just app insights for now. Giving up on logsink
const AppInsightsConnectionStringEnv = "APPLICATIONINSIGHTS_CONNECTION_STRING"

func Configure(ctx context.Context) (func(), error) {
	handlers := []slog.Handler{newContextHandler(slog.NewTextHandler(os.Stdout, nil))}

	closeFn := func() {} // can be a list if we have multiple

	if connectionString := os.Getenv(AppInsightsConnectionStringEnv); connectionString != "" {
		handler, err := appinsights.NewHandler(connectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("create app insights handler: %w", err)
		}
		handlers = append(handlers, newContextHandler(handler))
		closeFn = handler.Close
	}

	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))
	return recoverAndClose(ctx, closeFn), nil
}

func recoverAndClose(ctx context.Context, closeFn func()) func() {
	return func() {
		panicValue := recover()
		if panicValue != nil {
			slog.ErrorContext(ctx, "panic before logger flush",
				"panic", panicValue,
				"stack", string(debug.Stack()),
			)
		}

		closeFn()

		if panicValue != nil {
			panic(panicValue)
		}
	}
}
