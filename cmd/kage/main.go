package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	managedByAnnotationKey   = "managed-by"
	managedByAnnotationValue = "github.com/paulgmiller/kage"
	secretCommentPrefix      = "secret:"
	minSecretValueLength     = 5
)

// kage is my dumbed down vesion of https://github.com/getsops/sops
func main() {
	path := flag.String("secret-file", "secrets/envtest", "encrypted file to apply to k8s namespace")
	namespace := flag.String("ns", "", "k8s namespace")
	check := flag.Bool("check", false, "dump secret names")
	setSecret := flag.String("set", "", "add or update a secret value as secret/key=value")
	forreal := flag.Bool("apply", false, "don't actually apply secrets just print what would be done")
	flag.Parse()
	ctx := context.Background()

	if *forreal {
		log.Printf("THIS IS NOT A DRILL")
	}

	identities, err := loadSSHIdentities()
	if err != nil {
		log.Fatalf("need an identity %s", err)
	}
	ciphertext, err := os.Open(*path)
	if err != nil {
		log.Fatalf("can't open file %q, %s", *path, err)
	}
	defer func() {
		_ = ciphertext.Close()
	}()

	reader, err := age.Decrypt(ciphertext, identities...)
	if err != nil {
		log.Fatalf("decrypt file  %q: %s", *path, err)
	}

	plaintext, err := io.ReadAll(reader)
	if err != nil {
		log.Fatalf("read decrypted file %q: %s", *path, err)
	}

	secrets, err := secrets(bytes.NewReader(plaintext))
	if err != nil {
		panic(err)
	}

	if *setSecret != "" {
		secretName, key, value, err := parseSetArg(*setSecret)
		if err != nil {
			log.Fatal(err)
		}
		newSecretsFile, changed := setSecretValue(secrets, secretName, key, value)
		if !changed {
			log.Printf("%s/%s unchanged", secretName, key)
			return
		}
		if err := newSecretsFile.validate(); err != nil {
			log.Fatalf("updated secrets did not validate: %s", err)
		}
		recipients, err := loadSSHRecipients()
		if err != nil {
			log.Fatal(err)
		}
		file, err := os.OpenFile(*path, 0o600, os.FileMode(os.O_WRONLY))
		if err != nil {
			log.Fatal(err)
		}

		writer, err := age.Encrypt(file, recipients...)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			_ = writer.Close()
		}()

		if err := newSecretsFile.write(writer); err != nil {
			log.Fatal(err)
		}
		log.Printf("updated %s/%s in %s", secretName, key, *path)
		return
	}

	if *check {
		for _, secret := range secrets {
			fmt.Println(secret.Name)
			for _, line := range secret.Lines {
				if line.Key == "" {
					continue
				}
				fmt.Printf("  %s=%s\n", line.Key, maskedSecretValue(line.Value))
			}
			fmt.Println()
		}
		return
	}

	if namespace == nil || *namespace == "" {
		log.Fatal("namespace is required")
	}

	secretsK8s := toK8s(secrets)

	cfg, err := clientcmd.BuildConfigFromFlags(
		"",
		filepath.Join(os.Getenv("HOME"), ".kube", "config"),
	)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}
	secretapi := clientset.CoreV1().Secrets(*namespace)
	for _, secret := range secretsK8s {
		current, err := secretapi.Get(ctx, secret.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = secretapi.Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				log.Fatalf("failed to update %s: %s", secret.Name, err)
			}
			log.Printf("Created %s/%s", *namespace, secret.Name)
			continue
		}
		if !secretNeedsUpdate(current, secret) {
			continue
		}
		if !*forreal {
			log.Printf("would update %s/%s\n", *namespace, secret.Name)
			continue
		}
		secret.ResourceVersion = current.ResourceVersion
		_, err = secretapi.Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			log.Fatalf("failed to update %s: %s", secret.Name, err)
		}
		log.Printf("Updated %s/%s", *namespace, secret.Name)

	}
}

func secretNeedsUpdate(current, desired *corev1.Secret) bool {
	if current.Annotations[managedByAnnotationKey] != desired.Annotations[managedByAnnotationKey] {
		log.Printf("secret %s unmanged", desired.Name)
		return true
	}
	if len(current.Data) != len(desired.StringData) {
		log.Printf("secret %s key count mismatch", desired.Name)
		return true
	}
	for key, value := range desired.StringData {
		if !bytes.Equal(current.Data[key], []byte(value)) {
			log.Printf("secret %s key %s needs update", desired.Name, key)
			return true
		}
	}
	return false
}

func toK8s(secretVals secretsFile) []*corev1.Secret {
	var secrets []*corev1.Secret
	for _, vals := range secretVals {
		stringData := map[string]string{}
		for _, line := range vals.Lines {
			if line.Key == "" {
				continue
			}
			stringData[line.Key] = line.Value
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: vals.Name,
				Annotations: map[string]string{
					managedByAnnotationKey: managedByAnnotationValue,
				},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: stringData,
		}
		secrets = append(secrets, secret)
	}
	return secrets
}

func parseSetArg(arg string) (string, string, string, error) {
	secretAndKey, value, found := strings.Cut(arg, "=")
	if !found {
		return "", "", "", fmt.Errorf("set value must be secret/key=value")
	}
	secretName, key, found := strings.Cut(secretAndKey, "/")
	if !found {
		return "", "", "", fmt.Errorf("set value must be secret/key=value")
	}
	secretName = strings.TrimSpace(secretName)
	key = strings.TrimSpace(key)
	if secretName == "" || key == "" {
		return "", "", "", fmt.Errorf("set value must be secret/key=value")
	}
	if len(value) < minSecretValueLength {
		return "", "", "", fmt.Errorf("secret %s/%s must be at least %d characters", secretName, key, minSecretValueLength)
	}
	return secretName, key, value, nil
}

func setSecretValue(input secretsFile, secretName, key, value string) (secretsFile, bool) {
	var output secretsFile
	var secretFound bool
	for _, existingSecret := range input {
		if existingSecret.Name != secretName {
			output = append(output, existingSecret)
			continue
		}
		secretFound = true
		var lineFound bool
		for i, line := range existingSecret.Lines {
			if line.Key != key {
				continue
			}
			if line.Value == value {
				return secretsFile{}, false
			}
			line.Value = value
			existingSecret.Lines[i] = line
			lineFound = true
			continue
		}
		if !lineFound {
			existingSecret.Lines = append(existingSecret.Lines, secretLine{Key: key, Value: value})
		}
		output = append(output, existingSecret)
	}
	if !secretFound {
		output = append(output, secret{Name: secretName, Lines: []secretLine{{Key: key, Value: value}}})
	}

	return output, true
}

func formatSecretValue(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\n\r#\"'") {
		return strconv.Quote(value)
	}
	return value
}

func maskedSecretValue(value string) string {
	// invariant is value must be 5 or more characters, so this is safe
	return fmt.Sprintf("%s[%d]%s", value[:1], len(value), value[len(value)-1:])
}

func loadSSHRecipients() ([]age.Recipient, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("need a recipient")
	}
	path := filepath.Join(home, ".ssh", "id_ed25519.pub")

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ssh recipient %q: %w", path, err)
	}

	recipient, err := agessh.ParseRecipient(strings.TrimSpace(string(key)))
	if err != nil {
		return nil, fmt.Errorf("parse ssh recipient %q: %w", path, err)
	}

	return []age.Recipient{recipient}, nil
}

// share with internal/config?
func loadSSHIdentities() ([]age.Identity, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return []age.Identity{}, nil
	}
	path := filepath.Join(home, ".ssh", "id_ed25519")

	key, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []age.Identity{}, nil
		}
		return nil, err
	}

	identity, err := agessh.ParseIdentity(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh identity %q: %w", path, err)
	}

	return []age.Identity{identity}, nil
}
