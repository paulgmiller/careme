package deploy_test

import (
	"bytes"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Keep these lists in sync with config.LoadOnlyProviders and config.Load.
// Include the workloads' shared storage and telemetry requirements as well.
// Albertsons inventory searches use ALBERTSONS_SEARCH_SUBSCRIPTION_KEY.
var providerSecrets = []string{
	"albertsons", "brightdata-proxy", "kroger", "mnfoodclub", "storage", "walmart",
}

var fullSecrets = append(append([]string{}, providerSecrets...), "ai", "auth", "grafana")

type container struct {
	Name    string `yaml:"name"`
	Image   string `yaml:"image"`
	EnvFrom []struct {
		Prefix    string `yaml:"prefix"`
		SecretRef *struct {
			Name     string `yaml:"name"`
			Optional bool   `yaml:"optional"`
		} `yaml:"secretRef"`
	} `yaml:"envFrom"`
}

type podTemplate struct {
	Spec struct {
		Containers []container `yaml:"containers"`
	} `yaml:"spec"`
}

type manifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template    podTemplate `yaml:"template"`
		JobTemplate struct {
			Spec struct {
				Template podTemplate `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

func TestRequiredSecrets(t *testing.T) {
	files, err := filepath.Glob("*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	checked := map[string]int{}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			for {
				var doc manifest
				err := decoder.Decode(&doc)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				var template podTemplate
				switch doc.Kind {
				case "Deployment":
					template = doc.Spec.Template
				case "CronJob":
					template = doc.Spec.JobTemplate.Spec.Template
				default:
					continue
				}
				for _, c := range template.Spec.Containers {
					// These are the deployed binaries calling config.Load*.
					// Scraper images use their own configuration.
					image := strings.Split(strings.Split(path.Base(c.Image), "@")[0], ":")[0]
					var secrets []string
					switch image {
					case "careme":
						secrets = fullSecrets
					case "careme-ingredientreview":
						secrets = providerSecrets
					default:
						continue
					}
					checked[image]++
					mounted := map[string]bool{}
					for _, source := range c.EnvFrom {
						if ref := source.SecretRef; ref != nil && !ref.Optional && source.Prefix == "" {
							mounted[ref.Name] = true
						}
					}
					for _, secret := range secrets {
						assert.True(t, mounted[secret], "%s/%s container %s: missing required envFrom secret %q", doc.Kind, doc.Metadata.Name, c.Name, secret)
					}
				}
			}
		})
	}
	assert.Positive(t, checked["careme"], "no full-config workloads checked")
	assert.Positive(t, checked["careme-ingredientreview"], "no provider-config workloads checked")
}
