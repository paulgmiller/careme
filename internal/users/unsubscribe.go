package users

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"

	"careme/internal/config"
)

type unsubscribeTokenFactory struct {
	secret []byte
}

type UnsubscribeTokenFactory interface {
	UnsubscribeToken(userid string) string
}

func NewUnsubscribeTokenFactory(cfg config.Config) *unsubscribeTokenFactory {
	secret := cfg.Clerk.SecretKey // what else can we use
	return &unsubscribeTokenFactory{secret: []byte(secret)}
}

func (f *unsubscribeTokenFactory) UnsubscribeToken(userid string) string {
	// Why not just do SHA256(key || message)? Because plain hash functions like SHA-256 have structural properties that make naive keyed constructions risky, especially length-extension attacks for Merkle–Damgård hashes like SHA-256.
	mac := hmac.New(sha256.New, f.secret)
	mac.Write([]byte(userid))
	mac.Write([]byte("|"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func FakeUnsubscribeTokenFactory() *unsubscribeTokenFactory {
	return &unsubscribeTokenFactory{secret: []byte("fake_secret_for_testing")}
}
