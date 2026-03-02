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
	multi "github.com/samber/slog-multi" //this is getting a native version in newest golang
)

const appInsightsConnectionStringEnv = "APPLICATIONINSIGHTS_CONNECTION_STRING"

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
	close, err := configureLogger(ctx, logcfg)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer close()

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

func configureLogger(ctx context.Context, logcfg logsink.Config) (func(), error) {
	handlers := make([]slog.Handler, 0, 3)
	var logSinkCloser io.Closer
	if logcfg.Enabled() {
		handler, closer, err := logsink.NewJson(ctx, logcfg)
		if err != nil {
			return nil, fmt.Errorf("create logsink: %w", err)
		}
		handlers = append(handlers, handler)
		logSinkCloser = closer
	}
	var appinsightsHandler *appinsights.Handler
	if connectionString := os.Getenv(appInsightsConnectionStringEnv); connectionString != "" {
		handler, err := appinsights.NewHandler(connectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("create app insights handler: %w", err)
		}
		handlers = append(handlers, handler)
	}

	//you'd think this just be a slice of closedrs but app inights isn't an io.Closer because it returns no erro
	close := func() {
		if logSinkCloser != nil {
			if err := logSinkCloser.Close(); err != nil {
				fmt.Printf("error closing log sink: %s", err)
			}
		}
		if appinsightsHandler != nil {
			appinsightsHandler.Close()
		}
	}

	handlers = append(handlers, slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(slog.New(multi.Fanout(handlers...)))
	return close, nil
}
