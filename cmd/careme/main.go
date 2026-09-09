package main

import (
	"context"
	_ "embed"
	"flag"
	"log"
	"log/slog"
	"os"

	"careme/internal/campaigns"
	"careme/internal/config"
	"careme/internal/logsetup"
	"careme/internal/mail"
	"careme/internal/templates"
)

func main() {
	var serve, mailer, campaignJob bool
	var addr string

	// left for back compat does noting
	flag.BoolVar(&serve, "serve", false, "dead we always serve")
	flag.BoolVar(&mailer, "mail", false, "Run one-shot mail sender and exit")
	flag.StringVar(&addr, "addr", ":8080", "Address to bind in server mode")
	flag.BoolVar(&campaignJob, "campaigns", false, "Generate advertised recipes and images once and exit")
	flag.Parse()

	// todo just make these seperate images?
	if mailer && campaignJob {
		log.Fatal("-mail and -campaigns are mutually exclusive")
	}

	if err := os.MkdirAll("recipes", 0o755); err != nil {
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

	if err := templates.Init(cfg); err != nil {
		log.Fatalf("failed to initialize templates: %s", err)
	}

	if campaignJob {
		job, err := campaigns.NewService(cfg)
		if err == nil {
			err = job.RunOnce(ctx)
		}
		if err != nil {
			slog.ErrorContext(ctx, "campaign generation failed", "error", err)
			close()
			os.Exit(1)
		}
		return
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
