package main

import (
	"careme/internal/config"
	"careme/internal/logsink"
	"careme/internal/mail"
	"careme/internal/static"
	"careme/internal/templates"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"

	"github.com/openclosed-dev/slogan/appinsights"
	multi "github.com/samber/slog-multi"
)

const appInsightsConnectionStringEnv = "APPLICATIONINSIGHTS_CONNECTION_STRING"

type closerFn func()

func (f closerFn) Close() error {
	f()
	return nil
}

func main() {
	var serve, mailer bool
	var addr string

	//left for back compat does noting
	flag.BoolVar(&serve, "serve", false, "dead we always serve")
	flag.BoolVar(&mailer, "mail", false, "Run one-shot mail sender and exit")
	flag.StringVar(&addr, "addr", ":8080", "Address to bind in server mode")
	flag.Parse()

	if err := os.MkdirAll("recipes", 0755); err != nil {
		log.Fatalf("failed to create recipes directory: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	logcfg := logsink.ConfigFromEnv("logs")
	logClosers, err := configureLogger(ctx, logcfg)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeAll(logClosers)

	static.Init()
	if err := templates.Init(cfg, static.TailwindAssetPath); err != nil {
		log.Fatalf("failed to initialize templates: %s", err)
	}

	if mailer {
		mailer, err := mail.NewMailer(cfg)
		if err != nil {
			log.Fatalf("failed to create mailer: %v", err)
		}
		slog.InfoContext(ctx, "mail sender engaged (one-shot)")
		mailer.RunOnce(ctx)
		return
	}

	if err := runServer(cfg, logcfg, addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func configureLogger(ctx context.Context, logcfg logsink.Config) ([]io.Closer, error) {
	handlers := make([]slog.Handler, 0, 3)
	closers := make([]io.Closer, 0, 2)

	if logcfg.Enabled() {
		handler, closer, err := logsink.NewJson(ctx, logcfg)
		if err != nil {
			return nil, fmt.Errorf("create logsink: %w", err)
		}
		handlers = append(handlers, handler)
		closers = append(closers, closer)
	}

	if connectionString := os.Getenv(appInsightsConnectionStringEnv); connectionString != "" {
		handler, err := appinsights.NewHandler(connectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("create app insights handler: %w", err)
		}
		handlers = append(handlers, handler)
		closers = append(closers, closerFn(handler.Close))
	}

	if len(handlers) == 0 {
		return closers, nil
	}

	handlers = append(handlers, slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(slog.New(multi.Fanout(handlers...)))
	return closers, nil
}

func closeAll(closers []io.Closer) {
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].Close(); err != nil {
			slog.Error("failed to close logger", "error", err)
		}
	}
}
