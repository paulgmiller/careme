package main

import (
	"bufio"
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
	recipientsFilename       = "recipients.txt"
)

// kage is my dumbed down vesion of https://github.com/getsops/sops
func main() {
	path := flag.String("secret-file", "secrets/envtest", "encrypted file to apply to k8s namespace")
	namespace := flag.String("ns", "", "k8s namespace")
	check := flag.Bool("check", false, "dump secret names")
	setSecret := flag.String("set", "", "add or update a secret value as secret/key=value")
	reencrypt := flag.Bool("reencrypt", false, "re-encrypt the secret file using its recipients.txt")
	forreal := flag.Bool("apply", false, "actually apply secrets. Don't just print what would be done")
	flag.Parse()
	ctx := context.Background()
	if *setSecret != "" && *reencrypt {
		log.Fatal("set and reencrypt cannot be used together")
	}

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

	secrets, err := secrets(reader)
	if err != nil {
		panic(err)
	}

	if *reencrypt || *setSecret != "" {
		// todo let them specify
		recipientsPath := filepath.Join(filepath.Dir(*path), recipientsFilename)

		recipients, err := loadRecipients(recipientsPath)
		if err != nil {
			log.Fatal(err)
		}
		if *setSecret != "" {
			secretName, key, value, err := parseSetArg(*setSecret)
			if err != nil {
				log.Fatal(err)
			}
			var changed bool
			secrets, changed = setSecretValue(secrets, secretName, key, value)
			if !changed {
				log.Printf("%s/%s unchanged", secretName, key)
				return
			}
			log.Printf("updated %s/%s", secretName, key)
		}

		if err := secrets.validate(); err != nil {
			log.Fatalf("updated secrets did not validate: %s", err)
		}
		if err := encryptFile(*path, recipients, secrets.write); err != nil {
			log.Fatal(err)
		}
		log.Printf("updated %s", *path)
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

func loadRecipients(path string) ([]age.Recipient, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open recipients file %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	var recipients []age.Recipient
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var recipient age.Recipient
		if strings.HasPrefix(line, "ssh-") {
			recipient, err = agessh.ParseRecipient(line)
		} else {
			var parsed []age.Recipient
			parsed, err = age.ParseRecipients(strings.NewReader(line))
			if err == nil {
				recipient = parsed[0]
			}
		}
		if err != nil {
			return nil, fmt.Errorf("parse recipient %q in %q : %w", line, path, err)
		}
		recipients = append(recipients, recipient)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read recipients file %q: %w", path, err)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("no recipients in %q", path)
	}

	return recipients, nil
}

func encryptFile(path string, recipients []age.Recipient, writePlaintext func(io.Writer) error) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".kage-*")
	if err != nil {
		return fmt.Errorf("create temporary encrypted file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	writer, err := age.Encrypt(temporary, recipients...)
	if err != nil {
		return fmt.Errorf("start encryption: %w", err)
	}
	if err := writePlaintext(writer); err != nil {
		return fmt.Errorf("write encrypted file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish encryption: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close encrypted file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace encrypted file %q: %w", path, err)
	}
	return nil
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
