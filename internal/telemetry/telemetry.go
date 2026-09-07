// Package telemetry implements the local-first observability contract. The
// recorder accepts only allowlisted, low-cardinality facts. Network export is
// intentionally an injected opt-in concern; the default has no exporter.
package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const SchemaVersion = "autogit.telemetry/1"

type Event struct {
	SchemaVersion string `json:"schema_version"`
	At            string `json:"at"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Outcome       string `json:"outcome,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
}

type Recorder struct {
	mu      sync.Mutex
	path    string
	enabled bool
	maxFile int64
	events  []Event
}

// OTLPHTTPExporter is an explicit opt-in adapter for an OTLP/HTTP logs
// endpoint. It accepts already-sanitized Event values only; it never reads
// source, paths, prompts, credentials, or ambient telemetry context.
type OTLPHTTPExporter struct {
	Endpoint string
	Client   *http.Client
}

func (e OTLPHTTPExporter) Export(ctx context.Context, events []Event) error {
	if ctx == nil || e.Endpoint == "" {
		return errors.New("telemetry export context and endpoint are required")
	}
	parsed, err := url.Parse(e.Endpoint)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || len(e.Endpoint) > 512 {
		return errors.New("telemetry endpoint is invalid")
	}
	if len(events) > 1000 {
		return errors.New("telemetry export batch is too large")
	}
	records := make([]map[string]any, 0, len(events))
	for _, event := range events {
		event = sanitize(event)
		at, parseErr := time.Parse(time.RFC3339Nano, event.At)
		if parseErr != nil {
			return errors.New("telemetry timestamp is invalid")
		}
		attributes := []map[string]any{
			{"key": "autogit.kind", "value": map[string]any{"stringValue": event.Kind}},
			{"key": "autogit.name", "value": map[string]any{"stringValue": event.Name}},
			{"key": "autogit.outcome", "value": map[string]any{"stringValue": event.Outcome}},
		}
		if event.ErrorCode != "" {
			attributes = append(attributes, map[string]any{"key": "autogit.error_code", "value": map[string]any{"stringValue": event.ErrorCode}})
		}
		records = append(records, map[string]any{"timeUnixNano": at.UnixNano(), "attributes": attributes, "body": map[string]any{"stringValue": "autogit event"}})
	}
	body, err := json.Marshal(map[string]any{"resourceLogs": []any{map[string]any{
		"resource":  map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "autogit"}}}},
		"scopeLogs": []any{map[string]any{"scope": map[string]any{"name": "autogit.telemetry", "version": SchemaVersion}, "logRecords": records}},
	}}})
	if err != nil {
		return err
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telemetry exporter returned status %d", response.StatusCode)
	}
	return nil
}

func New(path string, enabled bool) (*Recorder, error) {
	if path == "" && enabled {
		return nil, errors.New("telemetry path is required when enabled")
	}
	return &Recorder{path: filepath.Clean(path), enabled: enabled, maxFile: 4 << 20}, nil
}

func (r *Recorder) Record(ctx context.Context, event Event) error {
	if r == nil || !r.enabled {
		return nil
	}
	if ctx == nil {
		return errors.New("telemetry context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	event = sanitize(event)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) >= 10000 {
		r.events = r.events[1:]
	}
	r.events = append(r.events, event)
	if r.path == "" {
		return nil
	}
	if info, err := os.Stat(r.path); err == nil && info.Size() >= r.maxFile {
		return errors.New("telemetry file reached its bounded size")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600) // #nosec G304 -- caller supplies the explicit local telemetry path.
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	return encoder.Encode(event)
}

func sanitize(event Event) Event {
	event.SchemaVersion = SchemaVersion
	if event.At == "" {
		event.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if len(event.Kind) > 32 {
		event.Kind = event.Kind[:32]
	}
	if len(event.Name) > 64 {
		event.Name = event.Name[:64]
	}
	if len(event.Outcome) > 32 {
		event.Outcome = event.Outcome[:32]
	}
	if len(event.ErrorCode) > 32 {
		event.ErrorCode = event.ErrorCode[:32]
	}
	if event.DurationMS < 0 {
		event.DurationMS = 0
	}
	return event
}

type Summary struct {
	Count int      `json:"count"`
	P50   int64    `json:"p50_ms"`
	P95   int64    `json:"p95_ms"`
	P99   int64    `json:"p99_ms"`
	Names []string `json:"names"`
}

func (r *Recorder) Summary() Summary {
	if r == nil {
		return Summary{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make([]int64, 0, len(r.events))
	names := map[string]bool{}
	for _, event := range r.events {
		if event.DurationMS > 0 {
			values = append(values, event.DurationMS)
		}
		if event.Name != "" {
			names[event.Name] = true
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	percentile := func(percent int) int64 {
		if len(values) == 0 {
			return 0
		}
		index := (len(values)*percent + 99) / 100
		if index < 1 {
			index = 1
		}
		if index > len(values) {
			index = len(values)
		}
		return values[index-1]
	}
	list := make([]string, 0, len(names))
	for name := range names {
		list = append(list, name)
	}
	sort.Strings(list)
	return Summary{Count: len(r.events), P50: percentile(50), P95: percentile(95), P99: percentile(99), Names: list}
}

func Read(path string, maxLines int) ([]Event, error) {
	if path == "" || maxLines < 1 || maxLines > 10000 {
		return nil, errors.New("invalid telemetry read limit")
	}
	file, err := os.Open(path) // #nosec G304 -- caller supplies an explicit local telemetry path.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 16<<10)
	result := []Event{}
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("invalid telemetry record")
		}
		result = append(result, sanitize(event))
		if len(result) >= maxLines {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
