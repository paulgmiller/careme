package main

import (
	"errors"
	"strings"

	"careme/pkg/kage"

	"k8s.io/apimachinery/pkg/util/validation"
)

func validateSecretNames(secrets kage.File) error {
	for _, secret := range secrets {
		if errs := validation.IsDNS1123Subdomain(secret.Name); len(errs) != 0 {
			return errors.New(strings.Join(errs, ";"))
		}
	}
	return nil
}
