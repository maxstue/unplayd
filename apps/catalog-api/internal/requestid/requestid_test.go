package requestid

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestLogHandlerAddsTheContextRequestID(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(LogHandler{Handler: slog.NewJSONHandler(&output, nil)}).With("service", "catalog-api").WithGroup("details")
	logger.InfoContext(With(context.Background(), "trace_1"), "with ID", "status", 200)
	logger.InfoContext(context.Background(), "without ID")
	decoder := json.NewDecoder(&output)
	for _, want := range []string{"trace_1", ""} {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		details, _ := entry["details"].(map[string]any)
		id, _ := details["requestId"].(string)
		if id != want || entry["service"] != "catalog-api" {
			t.Fatalf("entry = %v, want request ID %q", entry, want)
		}
	}
	if From(context.Background()) != "" {
		t.Fatal("context without ID returned one")
	}
}
