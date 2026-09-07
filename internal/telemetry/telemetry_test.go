package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelemetryIsAllowlistedBoundedAndLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	recorder, err := New(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(context.Background(), Event{Kind: "command", Name: "status", Outcome: "accepted", DurationMS: 10}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "prompt") || strings.Contains(string(data), "path") {
		t.Fatalf("telemetry leaked non-allowlisted data: %s", data)
	}
	if recorder.Summary().P95 != 10 {
		t.Fatalf("summary=%+v", recorder.Summary())
	}
	if _, err := Read(path, 10); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledTelemetryDoesNotCreateFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "telemetry.jsonl")
	recorder, err := New(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(context.Background(), Event{Name: "secret"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled telemetry created %v", err)
	}
}

func TestOTLPExportIsExplicitAndAllowlisted(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, 1<<16)
		n, _ := r.Body.Read(data)
		body = string(data[:n])
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	exporter := OTLPHTTPExporter{Endpoint: server.URL}
	if err := exporter.Export(context.Background(), []Event{{Kind: "command", Name: "status", Outcome: "accepted", At: "2026-09-07T00:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "service.name") || strings.Contains(body, "prompt") || strings.Contains(body, "path") {
		t.Fatalf("export body=%s", body)
	}
}
