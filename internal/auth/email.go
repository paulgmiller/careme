package auth

import (
	"fmt"

	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

// BuildMagicLinkEmail creates an email with a magic link for authentication
func BuildMagicLinkEmail(toEmail, token, baseURL string) *mail.SGMailV3 {
	from := mail.NewEmail("Careme", "noreply@careme.cooking")
	subject := "Your Careme Login Link"
	to := mail.NewEmail("", toEmail)

	magicLink := fmt.Sprintf("%s/login/verify?token=%s", baseURL, token)

	plainTextContent := fmt.Sprintf("Click the link below to sign in to Careme:\n\n%s\n\nThis link will expire in 15 minutes.", magicLink)

	htmlContent := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Login to Careme</title>
</head>
<body style="margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; background-color: #f5f5f5;">
    <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background-color: #f5f5f5; padding: 40px 20px;">
        <tr>
            <td align="center">
                <table role="presentation" width="600" cellpadding="0" cellspacing="0" style="background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1);">
                    <tr>
                        <td style="padding: 40px 40px 20px 40px; text-align: center;">
                            <h1 style="margin: 0; color: #333333; font-size: 28px; font-weight: 600;">Welcome to Careme</h1>
                        </td>
                    </tr>
                    <tr>
                        <td style="padding: 0 40px 20px 40px; text-align: center; color: #666666; font-size: 16px; line-height: 24px;">
                            <p style="margin: 0 0 20px 0;">Click the button below to sign in to your account:</p>
                        </td>
                    </tr>
                    <tr>
                        <td style="padding: 0 40px 30px 40px; text-align: center;">
                            <a href="%s" style="display: inline-block; padding: 14px 32px; background-color: #4F46E5; color: #ffffff; text-decoration: none; border-radius: 6px; font-size: 16px; font-weight: 600;">Sign In</a>
                        </td>
                    </tr>
                    <tr>
                        <td style="padding: 0 40px 20px 40px; text-align: center; color: #999999; font-size: 14px; line-height: 20px;">
                            <p style="margin: 0;">Or copy and paste this link into your browser:</p>
                            <p style="margin: 10px 0 0 0; word-break: break-all;"><a href="%s" style="color: #4F46E5; text-decoration: none;">%s</a></p>
                        </td>
                    </tr>
                    <tr>
                        <td style="padding: 20px 40px 40px 40px; text-align: center; color: #999999; font-size: 12px; line-height: 18px; border-top: 1px solid #eeeeee;">
                            <p style="margin: 0;">This link will expire in 15 minutes.</p>
                            <p style="margin: 10px 0 0 0;">If you didn't request this email, you can safely ignore it.</p>
                        </td>
                    </tr>
                </table>
            </td>
        </tr>
    </table>
</body>
</html>
`, magicLink, magicLink, magicLink)

	return mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
}
