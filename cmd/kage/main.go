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

	// adding gets tricky to retain comments
	if *check {
		for name, secret := range secrets {
			fmt.Println(name)
			for key, value := range secret {
				fmt.Printf("  %s=%s\n", key, maskedSecretValue(value))
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

type secret map[string]string

func secrets(r io.Reader) (map[string]secret, error) {
	sc := bufio.NewScanner(r)
	var currentSecret string
	secretVals := map[string]secret{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if comment, found := strings.CutPrefix(line, "#"); found {
			if secretName, found := strings.CutPrefix(comment, secretCommentPrefix); found {
				currentSecret = secretName
				if _, found := secretVals[currentSecret]; found {
					return nil, fmt.Errorf("duplicate secret comment %s", currentSecret)
				}
				secretVals[currentSecret] = secret{}
			}
			continue
		}
		if len(currentSecret) == 0 {
			continue
		}
		key, value, err := parseSecretLine(line)
		if err != nil {
			return nil, err
		}
		if key == "" {
			continue
		}
		secret := secretVals[currentSecret]
		if _, found := secret[key]; found {
			return nil, fmt.Errorf("duplicate secret key %s", key)
		}
		if len(value) < minSecretValueLength {
			return nil, fmt.Errorf("secret %s/%s must be at least %d characters", currentSecret, key, minSecretValueLength)
		}
		secret[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return secretVals, nil
}

func toK8s(secretVals map[string]secret) []*corev1.Secret {
	var secrets []*corev1.Secret
	for name, vals := range secretVals {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Annotations: map[string]string{
					managedByAnnotationKey: managedByAnnotationValue,
				},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: vals,
		}
		secrets = append(secrets, secret)
	}
	return secrets
}

func parseSecretLine(line string) (string, string, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", nil
	}

	key, rawValue, found := strings.Cut(trimmed, "=")
	if !found {
		return "", "", fmt.Errorf("invalid secret entry %q", line)
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("invalid secret entry %q", line)
	}

	value := stripInlineComment(rawValue)
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", "", err
		}
		value = unquoted
	} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}

	return key, value, nil
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
	lines, finalNewline := splitLines(string(input))
	currentSecret := ""
	secretStart := -1
	secretEnd := -1
	insertAt := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if comment, found := strings.CutPrefix(trimmed, "#"); found {
			if foundSecret, found := strings.CutPrefix(comment, secretCommentPrefix); found {
				foundSecret = strings.TrimSpace(foundSecret)
				if currentSecret == secretName && secretEnd == -1 {
					secretEnd = i
				}
				currentSecret = foundSecret
				if currentSecret == secretName {
					secretStart = i
					insertAt = i + 1
				}
			}
			continue
		}
		if currentSecret != secretName {
			continue
		}
		lineKey, lineValue, err := parseSecretLine(line)
		if err != nil {
			return nil, false, err
		}
		if lineKey == "" {
			insertAt = i + 1
			continue
		}
		insertAt = i + 1
		if lineKey != key {
			continue
		}
		if lineValue == value {
			return input, false, nil
		}
		lines[i] = replaceSecretLineValue(line, value)
		return joinLines(lines, finalNewline), true, nil
	}

	if secretStart != -1 {
		if secretEnd == -1 {
			secretEnd = len(lines)
		}
		if insertAt < secretStart+1 || insertAt > secretEnd {
			insertAt = secretEnd
		}
		lines = slicesInsert(lines, insertAt, fmt.Sprintf("%s=%s", key, formatSecretValue(value)))
		return joinLines(lines, finalNewline), true, nil
	}

	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	lines = append(lines, "#"+secretCommentPrefix+secretName, fmt.Sprintf("%s=%s", key, formatSecretValue(value)))
	return joinLines(lines, true), true, nil
}

func splitLines(input string) ([]string, bool) {
	finalNewline := strings.HasSuffix(input, "\n")
	if finalNewline {
		input = strings.TrimSuffix(input, "\n")
	}
	if input == "" {
		return nil, finalNewline
	}
	return strings.Split(input, "\n"), finalNewline
}

func joinLines(lines []string, finalNewline bool) []byte {
	output := strings.Join(lines, "\n")
	if finalNewline {
		output += "\n"
	}
	return []byte(output)
}

func slicesInsert(values []string, index int, value string) []string {
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}

func replaceSecretLineValue(line, value string) string {
	eq := strings.IndexByte(line, '=')
	if eq == -1 {
		return line
	}
	prefix := line[:eq+1]
	rawValue := line[eq+1:]
	commentIndex := inlineCommentIndex(rawValue)
	valuePart := rawValue
	commentPart := ""
	if commentIndex != -1 {
		valuePart = rawValue[:commentIndex]
		commentPart = rawValue[commentIndex:]
	}
	leading := valuePart[:len(valuePart)-len(strings.TrimLeft(valuePart, " \t"))]
	gap := valuePart[len(strings.TrimRight(valuePart, " \t")):]
	return prefix + leading + formatSecretValue(value) + gap + commentPart
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

func stripInlineComment(value string) string {
	if commentIndex := inlineCommentIndex(value); commentIndex != -1 {
		return value[:commentIndex]
	}
	return value
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
