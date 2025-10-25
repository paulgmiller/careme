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

func iterateUsers(users []users.User) {
	for _, user := range users {
		sendEmail(context.Background(), user)
	}
}

func sendEmail(ctx context.Context, user users.User) {

	from := mail.NewEmail("Chef", "chef@careme.cooking")
	subject := "Sending with SendGrid is Fun"
	to := mail.NewEmail("Example User", user.Email[0]) //todo email whole list
	htmlContent := "<a> <strong>Your recipes are ready!</strong>"
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
