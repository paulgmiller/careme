// https://app.sendgrid.com/guide/integrate/langs/go
// using SendGrid's Go Library
// https://github.com/sendgrid/sendgrid-go
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"

	"careme/internal/users"
)

func iterateUsers(ctx context.Context, userStorage users.Storage) {
	for { 
		users, err := userStorage.List(ctx)
		if  err != nil {
			slog.ErrorContext(ctx, "failed to list users", err)
			return
		}
		for _, user := range 
			sendEmail(context.Background(), user)
		}
	}
}

func sendEmail(ctx context.Context, user users.User) {

	from := mail.NewEmail("Chef", "chef@careme.cooking")
	subject := "Sending with SendGrid is Fun"

	htmlContent := "<a> <strong>Your recipes are ready!</strong>"
	for _, email := range user.Email {
		to := mail.NewEmail("Example User", email) //todo email whole list
		message := mail.NewSingleEmail(from, subject, to, "plainTextContent", htmlContent)
		client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
		// client.Request, _ = sendgrid.SetDataResidency(client.Request, "eu")
		// uncomment the above line if you are sending mail using a regional EU subuser
		response, err := client.Send(message)
		if err != nil {
			slog.ErrorContext(ctx, "mail error", err, "user", user.Email[0])
		} else {
			slog.InfoContext(ctx, "status", response.StatusCode, "body", response.Body, "headers", response.Headers)
		}
	}
}
