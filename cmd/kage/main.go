package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"careme/pkg/kage"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	managedByAnnotationKey   = "managed-by"
	managedByAnnotationValue = "github.com/paulgmiller/kage"
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

	if *forreal {
		log.Printf("THIS IS NOT A DRILL")
	}

	identities, err := kage.DefaultSSHIdentities()
	if err != nil {
		log.Fatalf("need an identity %s", err)
	}
	secrets, err := kage.ReadEncryptedFile(*path, identities)
	if err != nil {
		log.Fatal(err)
	}

	if *reencrypt || *setSecret != "" {
		// todo let them specify
		recipientsPath := filepath.Join(filepath.Dir(*path), recipientsFilename)

		recipients, err := kage.LoadRecipients(recipientsPath)
		if err != nil {
			log.Fatal(err)
		}
		if *setSecret != "" {
			secretName, key, value, err := parseSetArg(*setSecret)
			if err != nil {
				log.Fatal(err)
			}
			var changed bool
			secrets, changed = secrets.Set(secretName, key, value)
			if !changed {
				log.Printf("%s/%s unchanged", secretName, key)
				return
			}
			log.Printf("updated %s/%s", secretName, key)
		}

		if err := secrets.Validate(); err != nil {
			log.Fatalf("updated secrets did not validate: %s", err)
		}
		if err := kage.EncryptFile(*path, recipients, secrets); err != nil {
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

func toK8s(secretVals kage.File) []*corev1.Secret {
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
	if len(value) < kage.MinSecretValueLength {
		return "", "", "", fmt.Errorf(
			"secret %s/%s must be at least %d characters",
			secretName,
			key,
			kage.MinSecretValueLength,
		)
	}
	return secretName, key, value, nil
}

func maskedSecretValue(value string) string {
	// invariant is value must be 5 or more characters, so this is safe
	return fmt.Sprintf("%s[%d]%s", value[:1], len(value), value[len(value)-1:])
}
