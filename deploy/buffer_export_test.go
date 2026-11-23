package deploy_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type exportJob struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Schedule          string `yaml:"schedule"`
		TimeZone          string `yaml:"timeZone"`
		ConcurrencyPolicy string `yaml:"concurrencyPolicy"`
		JobTemplate       struct {
			Spec struct {
				BackoffLimit          int         `yaml:"backoffLimit"`
				ActiveDeadlineSeconds int         `yaml:"activeDeadlineSeconds"`
				Template              podTemplate `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

func TestBufferExportDeploysDailyInBothNamespaces(t *testing.T) {
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skip("envsubst is needed for deployment rendering")
	}
	source, err := os.Getwd()
	require.NoError(t, err)
	for _, namespace := range []string{"careme", "caremetest"} {
		t.Run(namespace, func(t *testing.T) {
			temporary := t.TempDir()
			// Exercise the real deploy script with stubbed Git and kubectl. Never contact a cluster.
			git := `#!/bin/sh
case "$1" in
 fetch|cat-file) exit 0 ;;
 rev-parse) echo 0123456789abcdef0123456789abcdef01234567 ;;
 show) file="${2#*:}"; cat "$DEPLOY_SOURCE/$file" ;;
 log) exit 0 ;;
 *) exit 1 ;;
esac
`
			kubectl := `#!/bin/sh
case "$1" in
 get) echo ghcr.io/paulgmiller/careme:0123456 ;;
 patch) cat ;;
 apply) printf '\n---\n' >> "$RENDERED_MANIFESTS"; cat >> "$RENDERED_MANIFESTS" ;;
 rollout) exit 0 ;;
 *) exit 1 ;;
esac
`
			require.NoError(t, os.WriteFile(filepath.Join(temporary, "git"), []byte(git), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(temporary, "kubectl"), []byte(kubectl), 0o755))
			rendered := filepath.Join(temporary, "rendered.yaml")
			command := exec.Command("bash", "deploy.sh", "test-ref", namespace)
			command.Env = append(os.Environ(), "PATH="+temporary+string(os.PathListSeparator)+os.Getenv("PATH"), "DEPLOY_SOURCE="+filepath.Dir(source), "RENDERED_MANIFESTS="+rendered)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "deploy output: %s", output)
			data, err := os.ReadFile(rendered)
			require.NoError(t, err)
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			found := false
			for {
				var job exportJob
				err := decoder.Decode(&job)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				if job.Metadata.Name != "careme-buffer-export" {
					continue
				}
				found = true
				assert.Equal(t, "CronJob", job.Kind)
				assert.Equal(t, "0 6 * * *", job.Spec.Schedule)
				assert.Equal(t, "America/Los_Angeles", job.Spec.TimeZone)
				assert.Equal(t, "Forbid", job.Spec.ConcurrencyPolicy)
				assert.Equal(t, 1, job.Spec.JobTemplate.Spec.BackoffLimit)
				assert.Equal(t, 900, job.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
				containers := job.Spec.JobTemplate.Spec.Template.Spec.Containers
				require.Len(t, containers, 1)
				assert.Equal(t, "ghcr.io/paulgmiller/careme-bufferexport:0123456", containers[0].Image)
				var secrets []string
				for _, env := range containers[0].EnvFrom {
					require.NotNil(t, env.SecretRef)
					secrets = append(secrets, env.SecretRef.Name)
				}
				assert.ElementsMatch(t, []string{"buffer", "storage"}, secrets)
			}
			assert.True(t, found, "Buffer export job must be included in deployment")
		})
	}
}

func TestBufferExporterImageIsIncludedInCI(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/go.yml")
	require.NoError(t, err)
	type image struct {
		Command string `yaml:"cmd_path"`
		Suffix  string `yaml:"image_suffix"`
	}
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []image `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &workflow))
	assert.Contains(t, workflow.Jobs["publish"].Strategy.Matrix.Include, image{Command: "./cmd/bufferexport", Suffix: "bufferexport"}, "the CronJob image must be built and published by CI")
}
