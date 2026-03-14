package main

import (
	"careme/internal/config"
	"careme/internal/logsetup"
	"careme/internal/mail"
	"careme/internal/static"
	"careme/internal/templates"
	"context"
	_ "embed"
	"flag"
	"log"
	"log/slog"
	"os"
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

	close, err := logsetup.Configure(ctx)
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

	if err := runServer(cfg, addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
