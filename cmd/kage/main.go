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
)

func main() {
	path := flag.String("secret-file", "secrets/envtest", "encrypted file to apply to k8s namespace")
	namespace := flag.String("ns", "", "k8s namespace")
	flag.Parse()
	ctx := context.Background()

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
	for _, secret := range secrets {
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
		return true
	}
	if len(current.Data) != len(desired.StringData) {
		return true
	}
	for key, value := range desired.StringData {
		if !bytes.Equal(current.Data[key], []byte(value)) {
			return true
		}
	}
	return false
}

func secrets(r io.Reader) ([]*corev1.Secret, error) {
	sc := bufio.NewScanner(r)
	var currentSecret string
	secretVals := map[string]map[string]string{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if comment, found := strings.CutPrefix(line, "#"); found {
			if secretName, found := strings.CutPrefix(comment, secretCommentPrefix); found {
				currentSecret = secretName
				if _, found := secretVals[currentSecret]; found {
					return nil, fmt.Errorf("duplicate secret comment %s", currentSecret)
				}
				secretVals[currentSecret] = map[string]string{}
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
		secret[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

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
	return secrets, nil
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

func stripInlineComment(value string) string {
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
			return value[:i]
		}
	}
	return value
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
