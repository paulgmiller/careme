package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"github.com/joho/godotenv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
		//check currents managed by
		if current.Annotations["managed-by"] != "github.com/paulgmiller/kage" {
			log.Fatalf("existing secret not managed by kage")
		}
		secret.ResourceVersion = current.ResourceVersion
		_, err = secretapi.Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			log.Fatalf("failed to update %s: %s", secret.Name, err)
		}
		//only update if actually changed
		log.Printf("Updated %s/%s", *namespace, secret.Name)

	}

}

func secrets(r io.Reader) ([]*corev1.Secret, error) {
	sc := bufio.NewScanner(r)
	var currentSecret string
	secretVals := map[string]map[string]string{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if strings.HasPrefix(comment, "secret:") {
				currentSecret = strings.TrimPrefix(comment, "secret:")
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
		//incredibly lazy and waseful come back and figure out yourself.
		kv, err := godotenv.Unmarshal(line)
		if err != nil {
			return nil, err
		}
		secret := secretVals[currentSecret]
		for key, value := range kv {
			if _, found := secret[key]; found {
				return nil, fmt.Errorf("duplicate secret key %s", key)
			}
			secret[key] = value
		}
		maps.Insert(secretVals[currentSecret], maps.All(kv))

	}

	var secrets []*corev1.Secret
	for name, vals := range secretVals {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Annotations: map[string]string{
					"managed-by": "github.com/paulgmiller/kage",
				},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: vals,
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
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
