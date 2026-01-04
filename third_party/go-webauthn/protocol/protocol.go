package protocol

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type AuthenticatorTransport string

type ResidentKeyRequirement string

type UserVerificationRequirement string

type AttestationConveyancePreference string

const (
	ResidentKeyRequirementPreferred ResidentKeyRequirement = "preferred"
)

const (
	VerificationRequired UserVerificationRequirement = "required"
)

const (
	PreferNoAttestation AttestationConveyancePreference = "none"
)

type AuthenticatorSelection struct {
	ResidentKey      ResidentKeyRequirement      `json:"residentKey,omitempty"`
	UserVerification UserVerificationRequirement `json:"userVerification,omitempty"`
}

type CredentialParameter struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

type RelyingPartyEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserEntity struct {
	ID          []byte `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type CredentialDescriptor struct {
	Type string `json:"type"`
	ID   []byte `json:"id"`
}

type PublicKeyCredentialCreationOptions struct {
	Challenge              []byte                          `json:"challenge"`
	RP                     RelyingPartyEntity              `json:"rp"`
	User                   UserEntity                      `json:"user"`
	PubKeyCredParams       []CredentialParameter           `json:"pubKeyCredParams"`
	ExcludeCredentials     []CredentialDescriptor          `json:"excludeCredentials,omitempty"`
	Timeout                int                             `json:"timeout,omitempty"`
	Attestation            AttestationConveyancePreference `json:"attestation,omitempty"`
	AuthenticatorSelection AuthenticatorSelection          `json:"authenticatorSelection,omitempty"`
}

type PublicKeyCredentialRequestOptions struct {
	Challenge        []byte                      `json:"challenge"`
	RPID             string                      `json:"rpId,omitempty"`
	Timeout          int                         `json:"timeout,omitempty"`
	UserVerification UserVerificationRequirement `json:"userVerification,omitempty"`
	AllowCredentials []CredentialDescriptor      `json:"allowCredentials,omitempty"`
}

func CreateChallenge() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Errorf("failed to generate challenge: %w", err))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
