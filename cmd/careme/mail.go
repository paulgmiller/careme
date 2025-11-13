// https://app.sendgrid.com/guide/integrate/langs/go
// using SendGrid's Go Library
// https://github.com/sendgrid/sendgrid-go
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/users"
)

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type mailer struct {
	userStorage *users.Storage
	generator   *recipes.Generator //interface requires making params public
	locServer   locServer
}

// TOD share some of this with web.go? goood for mocking?
func NewMailer(cfg *config.Config) (*mailer, error) {
	cache, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	userStorage := users.NewStorage(cache)

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return nil, fmt.Errorf("failed to create recipe generator: %w", err)
	}

	locationserver, err := locations.New(context.TODO(), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create location server: %w", err)
	}

	return &mailer{
		userStorage: userStorage,
		generator:   generator,
		locServer:   locationserver,
	}, nil
}

func (m *mailer) Iterate(ctx context.Context, duration time.Duration) {
	users, err := m.userStorage.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list users", err)
	} else {
		//toss this shit in a channel and use same channel to requeue
		for _, user := range users {
			m.sendEmail(ctx, user)
		}
	}
	ticker := time.NewTicker(duration)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			slog.InfoContext(ctx, "starting user email round")
			users, err := m.userStorage.List(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "failed to list users", err)
				continue //can we call back in 5 minutes?
			}
			//toss this shit in a channel and use same channel to requeue
			for _, user := range users {
				m.sendEmail(ctx, user)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (m *mailer) sendEmail(ctx context.Context, user users.User) {

	if user.FavoriteStore == "" {
		slog.InfoContext(ctx, "no favorite store", "user", user.ID)
		return
	}

	l, err := m.locServer.GetLocationByID(ctx, user.FavoriteStore)
	if err != nil {
		slog.ErrorContext(ctx, "error getting location", "location", user.FavoriteStore, "error", err)
		return
	}

	p := recipes.DefaultParams(l, time.Now().Add(-6*time.Hour)) //how do we get the timezone of the user?
	p.UserID = user.ID

	if err := m.generator.FromCache(ctx, p.Hash(), p, io.Discard); err == nil {
		//already generated. Assume we sent for now (need better atomic tracking)
		slog.InfoContext(ctx, "already emailed", "user", user.ID)
		return
	}

	if err := m.generator.GenerateRecipes(ctx, p); err != nil {
		slog.ErrorContext(ctx, "failed to generate recipes for user", "user", user.Email)
	}

	var buf bytes.Buffer
	if err := m.generator.FromCache(ctx, p.Hash(), p, &buf); err != nil {
		slog.ErrorContext(ctx, "failed to get generated recipes for user", "user", user.Email, "error", err)
		return
	}

	from := mail.NewEmail("Chef", "chef@careme.cooking")
	subject := "Your new recipes are ready!"

	plainTextContent := "Check out your new recipes at https://careme.cooking/recipes?hash=" + p.Hash()
	for _, email := range user.Email {
		to := mail.NewEmail("Example User", email) //todo email whole list
		message := mail.NewSingleEmail(from, subject, to, plainTextContent, buf.String())
		client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
		// client.Request, _ = sendgrid.SetDataResidency(client.Request, "eu")
		// uncomment the above line if you are sending mail using a regional EU subuser
		response, err := client.Send(message)
		if err != nil {
			slog.ErrorContext(ctx, "mail error", err, "user", user.Email[0])
		} else {
			slog.InfoContext(ctx, "status", response.StatusCode, "body", response.Body, "headers", response.Headers)
			// Todo shove something into cache so we don't resend.
		}
	}
}
