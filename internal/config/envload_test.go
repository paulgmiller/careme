package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"filippo.io/age"
	agessh "filippo.io/age/agessh"
)

const testSSHPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACB7qx7CGF0+RlAe2W0yhkiKlf71UMVcDaxCDfkSqtRO1QAAAJhNAJ9JTQCf
SQAAAAtzc2gtZWQyNTUxOQAAACB7qx7CGF0+RlAe2W0yhkiKlf71UMVcDaxCDfkSqtRO1Q
AAAEAIOeRpdKSm4SAwH+TzGtR01RQoGiR/PSEns26+wH1GXXurHsIYXT5GUB7ZbTKGSIqV
/vVQxVwNrEIN+RKq1E7VAAAAEXJvb3RAZTIzYzBmOTM1ZGFmAQIDBA==
-----END OPENSSH PRIVATE KEY-----`

const testSSHPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHurHsIYXT5GUB7ZbTKGSIqV/vVQxVwNrEIN+RKq1E7V test@careme"

func TestLoadRuntimeEnvLoadsDotAndEncryptedWithoutOverride(t *testing.T) {
	envLoadOnce = sync.Once{}
	t.Setenv("KEEP", "already")

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir(%q) error = %v", tmp, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	if err := os.WriteFile(".env", []byte("DOTENV_KEY=plain\nKEEP=from-dotenv\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(.env) error = %v", err)
	}

	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(.ssh) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte(testSSHPrivateKey), 0o600); err != nil {
		t.Fatalf("WriteFile(id_ed25519) error = %v", err)
	}

	if err := os.MkdirAll(filepath.Join("secrets"), 0o700); err != nil {
		t.Fatalf("MkdirAll(secrets) error = %v", err)
	}

	ciphertext, err := encryptWithRecipient("SECRET_KEY=encrypted\n", testSSHPublicKey)
	if err != nil {
		t.Fatalf("encryptWithRecipient() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join("secrets", "envtest"), ciphertext, 0o600); err != nil {
		t.Fatalf("WriteFile(secrets/envtest) error = %v", err)
	}

	if err := loadRuntimeEnv(); err != nil {
		t.Fatalf("loadRuntimeEnv() error = %v", err)
	}

	if got := os.Getenv("DOTENV_KEY"); got != "plain" {
		t.Fatalf("DOTENV_KEY = %q, want %q", got, "plain")
	}
	if got := os.Getenv("SECRET_KEY"); got != "encrypted" {
		t.Fatalf("SECRET_KEY = %q, want %q", got, "encrypted")
	}
	if got := os.Getenv("KEEP"); got != "already" {
		t.Fatalf("KEEP = %q, want %q", got, "already")
	}
}

func encryptWithRecipient(plaintext, publicKey string) ([]byte, error) {
	recipient, err := agessh.ParseRecipient(publicKey)
	if err != nil {
		return nil, err
	}

	var ciphertext bytes.Buffer
	writer, err := age.Encrypt(&ciphertext, recipient)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(writer, plaintext); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return ciphertext.Bytes(), nil
}
