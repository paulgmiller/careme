package logs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLogEntryUnmarshalJSON(t *testing.T) {
	jsonData := `{
		"time": "2024-01-15T10:30:00.000Z",
		"level": "INFO",
		"msg": "Test message",
		"source": {"file": "test.go", "line": 42},
		"user_id": "123",
		"request_id": "abc"
	}`

	var entry LogEntry
	err := json.Unmarshal([]byte(jsonData), &entry)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if entry.Time != "2024-01-15T10:30:00.000Z" {
		t.Errorf("Expected time to be 2024-01-15T10:30:00.000Z, got %s", entry.Time)
	}

	if entry.Level != "INFO" {
		t.Errorf("Expected level to be INFO, got %s", entry.Level)
	}

	if entry.Msg != "Test message" {
		t.Errorf("Expected msg to be 'Test message', got %s", entry.Msg)
	}

	if len(entry.Extra) != 2 {
		t.Errorf("Expected 2 extra fields, got %d", len(entry.Extra))
	}

	if entry.Extra["user_id"] != "123" {
		t.Errorf("Expected user_id to be '123', got %v", entry.Extra["user_id"])
	}

	if entry.Extra["request_id"] != "abc" {
		t.Errorf("Expected request_id to be 'abc', got %v", entry.Extra["request_id"])
	}
}

func TestParseLogStream(t *testing.T) {
	reader := &Reader{}
	logData := `{"time":"2024-01-15T10:30:00.000Z","level":"INFO","msg":"First message"}
{"time":"2024-01-15T10:35:00.000Z","level":"ERROR","msg":"Second message"}
{"time":"2024-01-15T09:00:00.000Z","level":"DEBUG","msg":"Old message"}
`

	since := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	logs, err := reader.parseLogStream(strings.NewReader(logData), since)
	if err != nil {
		t.Fatalf("Failed to parse log stream: %v", err)
	}

	// Should get 2 logs (the one at 09:00 should be filtered out)
	if len(logs) != 2 {
		t.Errorf("Expected 2 logs, got %d", len(logs))
	}

	if logs[0].Msg != "First message" {
		t.Errorf("Expected first message to be 'First message', got %s", logs[0].Msg)
	}

	if logs[1].Level != "ERROR" {
		t.Errorf("Expected second log level to be ERROR, got %s", logs[1].Level)
	}
}

func TestParseLogStreamWithInvalidJSON(t *testing.T) {
	reader := &Reader{}
	logData := `{"time":"2024-01-15T10:30:00.000Z","level":"INFO","msg":"Valid message"}
invalid json line
{"time":"2024-01-15T10:35:00.000Z","level":"ERROR","msg":"Another valid message"}
`

	since := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	logs, err := reader.parseLogStream(strings.NewReader(logData), since)
	if err != nil {
		t.Fatalf("Failed to parse log stream: %v", err)
	}

	// Should get 2 valid logs, skipping the invalid line
	if len(logs) != 2 {
		t.Errorf("Expected 2 logs, got %d", len(logs))
	}
}

func TestGetDatePrefixes(t *testing.T) {
	reader := &Reader{}

	// Test single day
	since := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	until := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)
	prefixes := reader.getDatePrefixes(since, until)

	if len(prefixes) != 1 {
		t.Errorf("Expected 1 prefix for same day, got %d", len(prefixes))
	}

	expected := "2024/01/15/"
	if prefixes[0] != expected {
		t.Errorf("Expected prefix %s, got %s", expected, prefixes[0])
	}

	// Test multiple days
	since = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	until = time.Date(2024, 1, 17, 14, 0, 0, 0, time.UTC)
	prefixes = reader.getDatePrefixes(since, until)

	if len(prefixes) != 3 {
		t.Errorf("Expected 3 prefixes for 3 days, got %d", len(prefixes))
	}

	expectedPrefixes := []string{"2024/01/15/", "2024/01/16/", "2024/01/17/"}
	for i, expected := range expectedPrefixes {
		if prefixes[i] != expected {
			t.Errorf("Expected prefix %s at index %d, got %s", expected, i, prefixes[i])
		}
	}
}

