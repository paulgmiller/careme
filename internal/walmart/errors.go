package walmart

import "fmt"

// StatusError captures non-2xx HTTP responses from Walmart APIs.
type StatusError struct {
	Operation  string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Body == "" {
		return fmt.Sprintf("%s request failed: status %d", e.Operation, e.StatusCode)
	}
	return fmt.Sprintf("%s request failed: status %d: %s", e.Operation, e.StatusCode, e.Body)
}
