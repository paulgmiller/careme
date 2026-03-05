package mail

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestACSEmailClientSend_BuildsSignedRequest(t *testing.T) {
	rawAccessKey := []byte("local-test-access-key")
	encodedAccessKey := base64.StdEncoding.EncodeToString(rawAccessKey)
	requestTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/emails:send" {
			t.Fatalf("path = %s, want /emails:send", r.URL.Path)
		}
		if got := r.URL.Query().Get("api-version"); got != acsEmailAPIVersion {
			t.Fatalf("api-version = %q, want %q", got, acsEmailAPIVersion)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		sum := sha256.Sum256(body)
		expectedContentHash := base64.StdEncoding.EncodeToString(sum[:])
		if got := r.Header.Get("x-ms-content-sha256"); got != expectedContentHash {
			t.Fatalf("x-ms-content-sha256 = %q, want %q", got, expectedContentHash)
		}

		expectedDate := requestTime.Format(http.TimeFormat)
		if got := r.Header.Get("x-ms-date"); got != expectedDate {
			t.Fatalf("x-ms-date = %q, want %q", got, expectedDate)
		}

		stringToSign := r.Method + "\n" + r.URL.RequestURI() + "\n" + expectedDate + ";" + r.Host + ";" + expectedContentHash
		h := hmac.New(sha256.New, rawAccessKey)
		if _, err := h.Write([]byte(stringToSign)); err != nil {
			t.Fatalf("failed to hash string to sign: %v", err)
		}
		expectedAuth := "HMAC-SHA256 SignedHeaders=x-ms-date;host;x-ms-content-sha256&Signature=" + base64.StdEncoding.EncodeToString(h.Sum(nil))
		if got := r.Header.Get("Authorization"); got != expectedAuth {
			t.Fatalf("authorization = %q, want %q", got, expectedAuth)
		}

		var payload acsEmailSendRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to decode request payload: %v", err)
		}
		if payload.SenderAddress != "chef@example.com" {
			t.Fatalf("senderAddress = %q, want chef@example.com", payload.SenderAddress)
		}
		if payload.Content.Subject != "Dinner plan" {
			t.Fatalf("subject = %q, want Dinner plan", payload.Content.Subject)
		}
		if len(payload.Recipients.To) != 2 {
			t.Fatalf("recipients = %d, want 2", len(payload.Recipients.To))
		}

		w.Header().Set("Operation-Location", "/emails/operations/abc123")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"abc123"}`))
	}))
	defer server.Close()

	client, err := newACSEmailClient(server.URL, "chef@example.com", encodedAccessKey)
	if err != nil {
		t.Fatalf("newACSEmailClient() error: %v", err)
	}
	client.httpClient = server.Client()
	client.now = func() time.Time { return requestTime }

	result, err := client.Send(context.Background(), EmailMessage{
		Subject:          "Dinner plan",
		PlainTextContent: "text",
		HTMLContent:      "<p>text</p>",
		To:               []string{"first@example.com", " second@example.com "},
	})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if result.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusAccepted)
	}
	if result.MessageID != "/emails/operations/abc123" {
		t.Fatalf("message id = %q, want /emails/operations/abc123", result.MessageID)
	}
}

func TestACSEmailClientSend_RejectsEmptyRecipients(t *testing.T) {
	encodedAccessKey := base64.StdEncoding.EncodeToString([]byte("local-test-access-key"))
	client, err := newACSEmailClient("https://example.communication.azure.com", "chef@example.com", encodedAccessKey)
	if err != nil {
		t.Fatalf("newACSEmailClient() error: %v", err)
	}

	_, err = client.Send(context.Background(), EmailMessage{
		Subject: "Dinner plan",
		To:      []string{" ", ""},
	})
	if err == nil || !strings.Contains(err.Error(), "at least one non-empty recipient is required") {
		t.Fatalf("expected recipient validation error, got %v", err)
	}
}
