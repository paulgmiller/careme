package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"careme/internal/config"
	caremail "careme/internal/mail"
	"careme/internal/providerregistry"
	"careme/internal/templates"
)

func main() {
	to := flag.String("to", "", "Careme user email that should receive today's recipes")
	plainText := flag.Bool("plain-text", false, "Send only the text/plain body for a placement test")
	flag.Parse()

	if *to == "" {
		log.Fatal("-to is required")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}
	if err := templates.Init(cfg); err != nil {
		log.Fatalf("initialize templates: %v", err)
	}

	sender, err := caremail.NewMailer(cfg, providerregistry.NewFactory(cfg))
	if err != nil {
		log.Fatal(err)
	}
	if err := sender.ForceSendToEmail(context.Background(), *to, caremail.SendOptions{PlainTextOnly: *plainText}); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Sent today's recipe email to %s\n", *to)
}
