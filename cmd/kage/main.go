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

	if *setSecret != "" {
		secretName, key, value, err := parseSetArg(*setSecret)
		if err != nil {
			log.Fatal(err)
		}
		updated, changed, err := setSecretValue(plaintext, secretName, key, value)
		if err != nil {
			log.Fatal(err)
		}
		if !changed {
			log.Printf("%s/%s unchanged", secretName, key)
			return
		}
		if _, err := secrets(bytes.NewReader(updated)); err != nil {
			log.Fatalf("updated secrets did not validate: %s", err)
		}
		recipients, err := loadSSHRecipients()
		if err != nil {
			log.Fatal(err)
		}
		ciphertext, err := encrypt(updated, recipients)
		if err != nil {
			log.Fatalf("encrypt updated file %q: %s", *path, err)
		}
		if err := os.WriteFile(*path, ciphertext, 0o600); err != nil {
			log.Fatalf("write updated file %q: %s", *path, err)
		}
		log.Printf("updated %s/%s in %s", secretName, key, *path)
		return
	}

	secrets, err := secrets(bytes.NewReader(plaintext))
	if err != nil {
		panic(err)
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

type secretsFile []secret

type secretLine struct {
	Key     string
	Value   string
	Comment string
}

type secret struct {
	Name  string
	Lines []secretLine
}

func secrets(r io.Reader) (secretsFile, error) {
	sc := bufio.NewScanner(r)
	var currentSecret string
	var secretVals secretsFile
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if comment, found := strings.CutPrefix(line, "#"); found {
			if secretName, found := strings.CutPrefix(comment, secretCommentPrefix); found {
				currentSecret = secretName
				if _, found := secretVals.find(currentSecret); found {
					return secretsFile{}, fmt.Errorf("duplicate secret comment %s", currentSecret)
				}
				secretVals = append(secretVals, secret{Name: currentSecret})
				continue
			}
			if currentSecret != "" {
				secretIndex, _ := secretVals.find(currentSecret)
				secret := secretVals[secretIndex]
				secret.Lines = append(secret.Lines, secretLine{Comment: line})
				secretVals[secretIndex] = secret
			}
			continue
		}
		if len(currentSecret) == 0 {
			continue
		}
		entry, err := parseSecretLine(line)
		if err != nil {
			return secretsFile{}, err
		}
		if entry.Key == "" {
			continue
		}
		secretIndex, _ := secretVals.find(currentSecret)
		secret := secretVals[secretIndex]
		secret.Lines = append(secret.Lines, entry)
		secretVals[secretIndex] = secret
	}
	if err := sc.Err(); err != nil {
		return secretsFile{}, err
	}
	if err := validateSecrets(secretVals); err != nil {
		return secretsFile{}, err
	}
	return secretVals, nil
}

func (secretVals secretsFile) find(name string) (int, bool) {
	for i, secret := range secretVals {
		if secret.Name == name {
			return i, true
		}
	}
	return -1, false
}

func validateSecrets(secretVals secretsFile) error {
	for _, secret := range secretVals {
		keys := map[string]struct{}{}
		for _, line := range secret.Lines {
			if line.Key == "" {
				continue
			}
			if _, found := keys[line.Key]; found {
				return fmt.Errorf("duplicate secret key %s", line.Key)
			}
			keys[line.Key] = struct{}{}
			if len(line.Value) < minSecretValueLength {
				return fmt.Errorf("secret %s/%s must be at least %d characters", secret.Name, line.Key, minSecretValueLength)
			}
		}
	}
	return nil
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

func parseSecretLine(line string) (secretLine, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return secretLine{}, nil
	}

	key, rawValue, found := strings.Cut(trimmed, "=")
	if !found {
		return secretLine{}, fmt.Errorf("invalid secret entry %q", line)
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return secretLine{}, fmt.Errorf("invalid secret entry %q", line)
	}

	value, comment := splitInlineComment(rawValue)
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return secretLine{}, err
		}
		value = unquoted
	} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}

	return secretLine{Key: key, Value: value, Comment: comment}, nil
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

func setSecretValue(input []byte, secretName, key, value string) ([]byte, bool, error) {
	secretVals, err := secrets(bytes.NewReader(input))
	if err != nil {
		return nil, false, err
	}
	secretIndex, found := secretVals.find(secretName)
	var sec secret
	if !found {
		sec = secret{Name: secretName}
		secretVals = append(secretVals, sec)
		secretIndex = len(secretVals) - 1
	} else {
		sec = secretVals[secretIndex]
	}
	for i, line := range sec.Lines {
		if line.Key != key {
			continue
		}
		if line.Value == value {
			return input, false, nil
		}
		line.Value = value
		sec.Lines[i] = line
		secretVals[secretIndex] = sec
		return serializeSecrets(secretVals), true, nil
	}
	sec.Lines = append(sec.Lines, secretLine{Key: key, Value: value})
	secretVals[secretIndex] = sec
	if err := validateSecrets(secretVals); err != nil {
		return nil, false, err
	}
	return serializeSecrets(secretVals), true, nil
}

func serializeSecrets(secretVals secretsFile) []byte {
	var out strings.Builder
	for i, secret := range secretVals {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString("#")
		out.WriteString(secretCommentPrefix)
		out.WriteString(secret.Name)
		out.WriteByte('\n')
		for _, line := range secret.Lines {
			if line.Key == "" {
				if line.Comment != "" {
					out.WriteString(line.Comment)
					out.WriteByte('\n')
				}
				continue
			}
			out.WriteString(line.Key)
			out.WriteByte('=')
			out.WriteString(formatSecretValue(line.Value))
			if line.Comment != "" {
				out.WriteByte(' ')
				out.WriteString(line.Comment)
			}
			out.WriteByte('\n')
		}
	}
	return []byte(out.String())
}

func formatSecretValue(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\n\r#\"'") {
		return strconv.Quote(value)
	}
	return value
}

func inlineCommentIndex(value string) int {
	var quote byte
	escaped := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
			return i
		}
	}
	return -1
}

func maskedSecretValue(value string) string {
	// invariant is value must be 5 or more characters, so this is safe
	return fmt.Sprintf("%s[%d]%s", value[:1], len(value), value[len(value)-1:])
}

func splitInlineComment(value string) (string, string) {
	if commentIndex := inlineCommentIndex(value); commentIndex != -1 {
		return value[:commentIndex], strings.TrimSpace(value[commentIndex:])
	}
	return value, ""
}

func encrypt(plaintext []byte, recipients []age.Recipient) ([]byte, error) {
	var ciphertext bytes.Buffer
	writer, err := age.Encrypt(&ciphertext, recipients...)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(plaintext); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return ciphertext.Bytes(), nil
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
