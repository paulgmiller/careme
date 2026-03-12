package logsetup

import (
	"careme/internal/logsink"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/openclosed-dev/slogan/appinsights"
	multi "github.com/samber/slog-multi"
)

const AppInsightsConnectionStringEnv = "APPLICATIONINSIGHTS_CONNECTION_STRING"

func Configure(ctx context.Context, logcfg logsink.Config) (func(), error) {
	handlers := make([]slog.Handler, 0, 3)
	var closers []func()

	if logcfg.Enabled() {
		handler, closer, err := logsink.NewJson(ctx, logcfg)
		if err != nil {
			return nil, fmt.Errorf("create logsink: %w", err)
		}
		handlers = append(handlers, handler)
		closers = append(closers, func() {
			if err := closer.Close(); err != nil {
				slog.Error("failed to close logsink", "error", err)
			}
		})
	}

	if connectionString := os.Getenv(AppInsightsConnectionStringEnv); connectionString != "" {
		handler, err := appinsights.NewHandler(connectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("create app insights handler: %w", err)
		}
		handlers = append(handlers, handler)
		closers = append(closers, handler.Close)
	}

	closeFn := func() {
		for _, closer := range closers {
			closer()
		}
	}

	handlers = append(handlers, slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(slog.New(multi.Fanout(handlers...)))
	return closeFn, nil
}
