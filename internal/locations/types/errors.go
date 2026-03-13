package types

import "errors"

type DisabledBackendError struct {
	Backend string
}

func (e *DisabledBackendError) Error() string {
	if e == nil || e.Backend == "" {
		return "location backend disabled"
	}
	return e.Backend + " location backend disabled"
}

func IsDisabledBackendError(err error) bool {
	var target *DisabledBackendError
	return errors.As(err, &target)
}
