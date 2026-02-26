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
	"log"
	"log/slog"
	"os"

	multi "github.com/samber/slog-multi"
)

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
	if logcfg.Enabled() {
		handler, closer, err := logsink.NewJson(ctx, logcfg)
		if err != nil {
			log.Fatalf("failed to create logsink: %v", err)
		}
		defer func() {
			if err := closer.Close(); err != nil {
				slog.Error("failed to close logsink", "error", err)
			}
		}()
		slog.SetDefault(slog.New(multi.Fanout(handler, slog.NewTextHandler(os.Stdout, nil))))
		// log.SetOutput(os.Stdout) // https://github.com/golang/go/issues/61892

	}

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
