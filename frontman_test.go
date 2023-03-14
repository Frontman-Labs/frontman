package frontman

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateRedirectServer(t *testing.T) {
	redirectAddr := "0.0.0.0:8000"
	redirectServer := createRedirectServer("0.0.0.0:80", redirectAddr)

	// Create a test request to the redirect server
	req, err := http.NewRequest("GET", "http://example.com/foo", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test response recorder
	rr := httptest.NewRecorder()

	// Call the redirect server's handler function
	redirectServer.Handler.ServeHTTP(rr, req)

	// Check that the response has a 301 status code
	if status := rr.Code; status != http.StatusMovedPermanently {
		t.Errorf("Unexpected status code: got %v, expected %v", status, http.StatusMovedPermanently)
	}

	// Check that the response includes a "Location" header with the expected value
	expectedURL := "https://example.com/foo"
	if location := rr.Header().Get("Location"); location != expectedURL {
		t.Errorf("Unexpected Location header value: got %v, expected %v", location, expectedURL)
	}
}
