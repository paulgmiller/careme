package recipes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedirectToHash(t *testing.T) {
	// Create a ResponseRecorder to record the response
	rr := httptest.NewRecorder()
	// Create a dummy request
	req := httptest.NewRequest("GET", "/dummy", nil)

	hash := "testhash"
	redirectToHash(rr, req, hash, true)

	// Check the status code
	if status := rr.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusSeeOther)
	}

	// Check the Location header
	expectedLocation := fmt.Sprintf("/recipes?h=%s&start=", hash)
	location := rr.Header().Get("Location")
	if !strings.HasPrefix(location, expectedLocation) {
		t.Errorf("handler returned wrong location: got %v want prefix %v", location, expectedLocation)
	}
}
