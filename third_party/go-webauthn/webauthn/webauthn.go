package webauthn

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
)

// User mirrors the go-webauthn user interface used by our handlers.
type User interface {
	WebAuthnID() []byte
	WebAuthnName() string
	WebAuthnDisplayName() string
	WebAuthnIcon() string
	WebAuthnCredentials() []Credential
}

type Config struct {
	RPDisplayName string
	RPID          string
	RPOrigin      string
}

type WebAuthn struct {
	config *Config
}

type SessionData struct {
	Challenge        []byte
	UserID           []byte
	RPID             string
	Origin           string
	AllowCredentials [][]byte
	UserVerification protocol.UserVerificationRequirement
}

type Authenticator struct {
	SignCount uint32
}

type Credential struct {
	ID              []byte
	PublicKey       []byte
	AttestationType string
	Transport       []protocol.AuthenticatorTransport
	Authenticator   Authenticator
}

type registrationConfig struct {
	selection  protocol.AuthenticatorSelection
	preference protocol.AttestationConveyancePreference
}

type RegistrationOption func(*registrationConfig)

type loginConfig struct {
	userVerification protocol.UserVerificationRequirement
}

type LoginOption func(*loginConfig)

func WithAuthenticatorSelection(selection protocol.AuthenticatorSelection) RegistrationOption {
	return func(cfg *registrationConfig) {
		cfg.selection = selection
	}
}

func WithConveyancePreference(pref protocol.AttestationConveyancePreference) RegistrationOption {
	return func(cfg *registrationConfig) {
		cfg.preference = pref
	}
}

func WithUserVerification(req protocol.UserVerificationRequirement) LoginOption {
	return func(cfg *loginConfig) {
		cfg.userVerification = req
	}
}

func New(cfg *Config) (*WebAuthn, error) {
	if cfg == nil {
		return nil, errors.New("missing webauthn config")
	}
	if cfg.RPID == "" || cfg.RPOrigin == "" || cfg.RPDisplayName == "" {
		return nil, errors.New("invalid webauthn config")
	}
	return &WebAuthn{config: cfg}, nil
}

func (w *WebAuthn) BeginRegistration(user User, opts ...RegistrationOption) (any, *SessionData, error) {
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, nil, err
	}

	cfg := registrationConfig{preference: protocol.PreferNoAttestation}
	for _, opt := range opts {
		opt(&cfg)
	}

	exclude := make([]protocol.CredentialDescriptor, 0)
	for _, cred := range user.WebAuthnCredentials() {
		exclude = append(exclude, protocol.CredentialDescriptor{Type: "public-key", ID: cred.ID})
	}

	options := struct {
		PublicKey protocol.PublicKeyCredentialCreationOptions `json:"publicKey"`
	}{PublicKey: protocol.PublicKeyCredentialCreationOptions{
		Challenge: challenge,
		RP: protocol.RelyingPartyEntity{
			ID:   w.config.RPID,
			Name: w.config.RPDisplayName,
		},
		User: protocol.UserEntity{
			ID:          user.WebAuthnID(),
			Name:        user.WebAuthnName(),
			DisplayName: user.WebAuthnDisplayName(),
		},
		PubKeyCredParams: []protocol.CredentialParameter{
			{Type: "public-key", Alg: -7},
			{Type: "public-key", Alg: -257},
		},
		ExcludeCredentials:     exclude,
		Timeout:                60000,
		Attestation:            cfg.preference,
		AuthenticatorSelection: cfg.selection,
	}}

	session := &SessionData{
		Challenge:        challenge,
		UserID:           user.WebAuthnID(),
		RPID:             w.config.RPID,
		Origin:           w.config.RPOrigin,
		UserVerification: cfg.selection.UserVerification,
	}
	return options, session, nil
}

func (w *WebAuthn) FinishRegistration(user User, session SessionData, r *http.Request) (*Credential, error) {
	var credential registrationResponse
	if err := json.NewDecoder(r.Body).Decode(&credential); err != nil {
		return nil, fmt.Errorf("invalid credential payload: %w", err)
	}

	clientDataBytes, err := base64.RawURLEncoding.DecodeString(credential.Response.ClientDataJSON)
	if err != nil {
		return nil, errors.New("invalid client data")
	}
	if err := verifyClientData(clientDataBytes, session, "webauthn.create"); err != nil {
		return nil, err
	}

	publicKeyBytes, err := base64.RawURLEncoding.DecodeString(credential.Response.PublicKey)
	if err != nil || len(publicKeyBytes) == 0 {
		return nil, errors.New("missing public key from authenticator")
	}

	credentialID, err := base64.RawURLEncoding.DecodeString(credential.RawID)
	if err != nil {
		return nil, errors.New("invalid credential id")
	}

	return &Credential{
		ID:              credentialID,
		PublicKey:       publicKeyBytes,
		AttestationType: string(protocol.PreferNoAttestation),
		Authenticator:   Authenticator{SignCount: 0},
	}, nil
}

func (w *WebAuthn) BeginLogin(user User, opts ...LoginOption) (any, *SessionData, error) {
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, nil, err
	}

	cfg := loginConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	creds := user.WebAuthnCredentials()
	allow := make([]protocol.CredentialDescriptor, 0, len(creds))
	allowIDs := make([][]byte, 0, len(creds))
	for _, cred := range creds {
		allow = append(allow, protocol.CredentialDescriptor{Type: "public-key", ID: cred.ID})
		allowIDs = append(allowIDs, cred.ID)
	}

	options := struct {
		PublicKey protocol.PublicKeyCredentialRequestOptions `json:"publicKey"`
	}{PublicKey: protocol.PublicKeyCredentialRequestOptions{
		Challenge:        challenge,
		RPID:             w.config.RPID,
		Timeout:          60000,
		UserVerification: cfg.userVerification,
		AllowCredentials: allow,
	}}

	session := &SessionData{
		Challenge:        challenge,
		UserID:           user.WebAuthnID(),
		RPID:             w.config.RPID,
		Origin:           w.config.RPOrigin,
		AllowCredentials: allowIDs,
		UserVerification: cfg.userVerification,
	}
	return options, session, nil
}

func (w *WebAuthn) FinishLogin(user User, session SessionData, r *http.Request) (*Credential, error) {
	var credential assertionResponse
	if err := json.NewDecoder(r.Body).Decode(&credential); err != nil {
		return nil, errors.New("invalid credential payload")
	}

	clientDataBytes, err := base64.RawURLEncoding.DecodeString(credential.Response.ClientDataJSON)
	if err != nil {
		return nil, errors.New("invalid client data")
	}
	if err := verifyClientData(clientDataBytes, session, "webauthn.get"); err != nil {
		return nil, err
	}

	authData, err := base64.RawURLEncoding.DecodeString(credential.Response.AuthenticatorData)
	if err != nil {
		return nil, errors.New("invalid authenticator data")
	}

	signature, err := base64.RawURLEncoding.DecodeString(credential.Response.Signature)
	if err != nil {
		return nil, errors.New("invalid signature")
	}

	credentialID, err := base64.RawURLEncoding.DecodeString(credential.RawID)
	if err != nil {
		return nil, errors.New("invalid credential id")
	}

	var stored *Credential
	for _, cred := range user.WebAuthnCredentials() {
		if bytes.Equal(cred.ID, credentialID) {
			stored = &cred
			break
		}
	}
	if stored == nil {
		return nil, errors.New("unknown passkey")
	}

	signCount, err := verifyAssertion(stored.PublicKey, authData, clientDataBytes, signature, session.RPID)
	if err != nil {
		return nil, err
	}

	stored.Authenticator.SignCount = signCount
	return stored, nil
}

func verifyClientData(data []byte, session SessionData, expectedType string) error {
	var cd clientData
	if err := json.Unmarshal(data, &cd); err != nil {
		return fmt.Errorf("invalid client data format: %w", err)
	}
	if cd.Type != expectedType {
		return fmt.Errorf("unexpected client data type")
	}
	if cd.Origin != session.Origin {
		return fmt.Errorf("origin mismatch")
	}
	challenge, err := base64.RawURLEncoding.DecodeString(cd.Challenge)
	if err != nil {
		return fmt.Errorf("unable to decode challenge")
	}
	if !compareBytes(challenge, session.Challenge) {
		return fmt.Errorf("challenge mismatch")
	}
	return nil
}

func verifyAssertion(publicKey, authData, clientDataJSON, signature []byte, rpID string) (uint32, error) {
	if len(authData) < 37 {
		return 0, errors.New("authenticator data too short")
	}
	rpHash := authData[:32]
	flags := authData[32]
	signCount := binary.BigEndian.Uint32(authData[33:37])

	expectedHash := sha256.Sum256([]byte(rpID))
	if !compareBytes(rpHash, expectedHash[:]) {
		return 0, errors.New("rp hash mismatch")
	}
	const userVerified = 0x04
	if flags&userVerified == 0 {
		return 0, errors.New("user verification not present")
	}

	clientHash := sha256.Sum256(clientDataJSON)
	signed := append(authData, clientHash[:]...)
	digest := sha256.Sum256(signed)

	pk, err := x509.ParsePKIXPublicKey(publicKey)
	if err != nil {
		return 0, fmt.Errorf("invalid stored public key: %w", err)
	}

	switch key := pk.(type) {
	case *ecdsa.PublicKey:
		var ecdsaSig struct {
			R, S *big.Int
		}
		if _, err := asn1.Unmarshal(signature, &ecdsaSig); err != nil {
			return 0, fmt.Errorf("invalid ecdsa signature: %w", err)
		}
		if !ecdsa.Verify(key, digest[:], ecdsaSig.R, ecdsaSig.S) {
			return 0, errors.New("ecdsa signature verification failed")
		}
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
			return 0, fmt.Errorf("rsa signature verification failed: %w", err)
		}
	default:
		return 0, errors.New("unsupported public key type")
	}

	return signCount, nil
}

func compareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

type registrationResponse struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		AttestationObject string `json:"attestationObject"`
		ClientDataJSON    string `json:"clientDataJSON"`
		PublicKey         string `json:"publicKey"`
	} `json:"response"`
}

type assertionResponse struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		AuthenticatorData string `json:"authenticatorData"`
		ClientDataJSON    string `json:"clientDataJSON"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"userHandle"`
	} `json:"response"`
}
