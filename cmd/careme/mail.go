// https://app.sendgrid.com/guide/integrate/langs/go
// using SendGrid's Go Library
// https://github.com/sendgrid/sendgrid-go
package main

import (
	"bytes"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/users"
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type emailClient interface {
	Send(message *mail.SGMailV3) (*rest.Response, error)
}

type mailer struct {
	cache       cache.Cache
	userStorage *users.Storage
	generator   *recipes.Generator // interface requires making params public
	locServer   locServer
	client      emailClient
}

// TODO share some of this with web.go? good for mocking?
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

	// shove into cfg?
	sendgridkey := os.Getenv("SENDGRID_API_KEY")
	if sendgridkey == "" {
		return nil, fmt.Errorf("SENDGRID_API_KEY environment variable is not set")
	}

	return &mailer{
		cache:       cache,
		userStorage: userStorage,
		generator:   generator.(*recipes.Generator), // TODO do better
		locServer:   locationserver,
		client:      sendgrid.NewSendClient(sendgridkey),
	}, nil
}

func (m *mailer) Iterate(ctx context.Context, duration time.Duration) {
	users, err := m.userStorage.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list users", "error", err.Error())
	} else {
		// toss this in a channel and use same channel to requeue
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
				slog.ErrorContext(ctx, "failed to list users", "error", err.Error())
				continue // can we call back in 5 minutes?
			}
			// toss this shit in a channel and use same channel to requeue
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
		slog.ErrorContext(ctx, "error getting location", "location", user.FavoriteStore, "error", err.Error())
		return
	}

	p := recipes.DefaultParams(l, time.Now().Add(-6*time.Hour)) // how do we get the timezone of the user?
	p.UserID = user.ID
	rio := recipes.IO(m.cache)
	if _, err := rio.FromCache(ctx, p.Hash()); err == nil {
		// already generated. Assume we sent for now (need better atomic tracking)
		// must include user id in tracking.
		slog.InfoContext(ctx, "already emailed", "user", user.ID)
		return
	}

	for _, last := range user.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			continue
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	shoppingList, err := m.generator.GenerateRecipes(ctx, p)
	if err != nil {
		slog.ErrorContext(ctx, "failed to generate recipes for user", "user", user.Email)
	}
	// combine here save recipes with html
	rio.SaveShoppingList(ctx, shoppingList, p)

	var buf bytes.Buffer
	recipes.FormatMail(p, *shoppingList, &buf)

	from := mail.NewEmail("Chef", "chef@careme.cooking")
	subject := "Your new recipes are ready!"

	plainTextContent := "Check out your new recipes at https://careme.cooking/recipes?h=" + p.Hash()
	for _, email := range user.Email {
		to := mail.NewEmail("Example User", email) // todo email whole list
		message := mail.NewSingleEmail(from, subject, to, plainTextContent, buf.String())

		// client.Request, _ = sendgrid.SetDataResidency(client.Request, "eu")
		// uncomment the above line if you are sending mail using a regional EU subuser
		response, err := m.client.Send(message)
		if err != nil {
			slog.ErrorContext(ctx, "mail error", "error", err.Error(), "user", user.Email[0])
		} else {
			slog.InfoContext(ctx, "status", slog.Int("status", response.StatusCode), "body", response.Body, "headers", response.Headers)
			// Todo shove something into cache so we don't resend.
		}
	}
}
