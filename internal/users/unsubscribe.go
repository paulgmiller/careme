package users

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"os"
	"strings"

	utypes "careme/internal/users/types"
)

func unsubscribeSecret() string {
	if v := strings.TrimSpace(os.Getenv("CLERK_SECRET_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("AI_API_KEY")); v != "" {
		return v
	}
	return "careme-unsubscribe-secret"
}

func UnsubscribeToken(user utypes.User) string {
	mac := hmac.New(sha256.New, []byte(unsubscribeSecret()))
	mac.Write([]byte(user.ID))
	mac.Write([]byte("|"))
	if len(user.Email) > 0 {
		mac.Write([]byte(strings.TrimSpace(strings.ToLower(user.Email[0]))))
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func ValidUnsubscribeToken(user utypes.User, token string) bool {
	want := UnsubscribeToken(user)
	return subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1
}
