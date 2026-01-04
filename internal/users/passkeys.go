package users

import (
	"bytes"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

func (u *User) PrimaryEmail() string {
	if len(u.Email) > 0 {
		return u.Email[0]
	}
	return u.ID
}

type webAuthnUser struct {
	user *User
}

func newWebAuthnUser(user *User) webAuthnUser {
	return webAuthnUser{user: user}
}

func (u webAuthnUser) WebAuthnID() []byte {
	return []byte(u.user.ID)
}

func (u webAuthnUser) WebAuthnName() string {
	return u.user.PrimaryEmail()
}

func (u webAuthnUser) WebAuthnDisplayName() string {
	return u.user.PrimaryEmail()
}

func (u webAuthnUser) WebAuthnIcon() string {
	return ""
}

func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	creds := make([]webauthn.Credential, 0, len(u.user.Passkeys))
	for _, pk := range u.user.Passkeys {
		creds = append(creds, passkeyToCredential(pk))
	}
	return creds
}

func ReplacePasskeyFromCredential(u *User, credential *webauthn.Credential) {
	transports := make([]string, len(credential.Transport))
	for i, transport := range credential.Transport {
		transports[i] = string(transport)
	}
	replacePasskey(u, credential.ID, credential.PublicKey, credential.Authenticator.SignCount, credential.AttestationType, transports)
}

func UpdatePasskeyFromCredential(u *User, credential *webauthn.Credential) {
	transports := make([]string, len(credential.Transport))
	for i, transport := range credential.Transport {
		transports[i] = string(transport)
	}
	replacePasskey(u, credential.ID, credential.PublicKey, credential.Authenticator.SignCount, credential.AttestationType, transports)
}

func replacePasskey(u *User, credentialID, publicKey []byte, signCount uint32, attestationType string, transports []string) {
	now := time.Now()
	for i := range u.Passkeys {
		if bytes.Equal(u.Passkeys[i].CredentialID, credentialID) {
			u.Passkeys[i].PublicKey = publicKey
			u.Passkeys[i].SignCount = signCount
			u.Passkeys[i].AttestationType = attestationType
			u.Passkeys[i].Transports = transports
			u.Passkeys[i].LastUsed = now
			return
		}
	}
	u.Passkeys = append(u.Passkeys, Passkey{
		CredentialID:    credentialID,
		PublicKey:       publicKey,
		SignCount:       signCount,
		AttestationType: attestationType,
		Transports:      transports,
		CreatedAt:       now,
		LastUsed:        now,
	})
}

func passkeyToCredential(pk Passkey) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, len(pk.Transports))
	for i, transport := range pk.Transports {
		transports[i] = protocol.AuthenticatorTransport(transport)
	}
	return webauthn.Credential{
		ID:              pk.CredentialID,
		PublicKey:       pk.PublicKey,
		AttestationType: pk.AttestationType,
		Transport:       transports,
		Authenticator: webauthn.Authenticator{
			SignCount: pk.SignCount,
		},
	}
}
