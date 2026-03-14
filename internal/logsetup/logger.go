package logsetup

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/openclosed-dev/slogan/appinsights"
)

// just app insights for now. Giving up on logsink
const AppInsightsConnectionStringEnv = "APPLICATIONINSIGHTS_CONNECTION_STRING"

func Configure(ctx context.Context) (func(), error) {
	handlers := []slog.Handler{slog.NewTextHandler(os.Stdout, nil)}

	closeFn := func() {} // can be a list if we have multiple

	if connectionString := os.Getenv(AppInsightsConnectionStringEnv); connectionString != "" {
		handler, err := appinsights.NewHandler(connectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("create app insights handler: %w", err)
		}
		handlers = append(handlers, handler)
		closeFn = handler.Close
	}

	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))
	return closeFn, nil
}
