package safewayads

import "testing"

func TestStorageContainerName(t *testing.T) {
	t.Setenv(containerEnvVarName, "")
	if got := storageContainerName(); got != defaultContainer {
		t.Fatalf("storageContainerName() = %q, want %q", got, defaultContainer)
	}

	t.Setenv(containerEnvVarName, "safewayads")
	if got := storageContainerName(); got != "safewayads" {
		t.Fatalf("storageContainerName() = %q, want %q", got, "safewayads")
	}
}
