package types

import (
	"errors"
	"fmt"
)

var errDisabledBackend = errors.New("location backend is disabled")

func DisabledBackendError(backend string) error {
	return fmt.Errorf("%w: %s", errDisabledBackend, backend)
}
func IsDisabledBackendError(err error) bool {
	return errors.Is(err, errDisabledBackend)
}
