// Package compatibility owns the release-facing support-window contract.
// Compatibility is explicit data so expiry can fail a release before an
// advertised client, tool, or protocol silently drifts.
package compatibility

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

type Window struct {
	Minimum  string `json:"minimum,omitempty"`
	API      string `json:"api_version,omitempty"`
	Spec     string `json:"spec_version,omitempty"`
	Schema   string `json:"schema_major,omitempty"`
	ReviewBy string `json:"review_by"`
}

type Manifest struct {
	SchemaVersion      string            `json:"schema_version"`
	AdapterRegistry    string            `json:"adapter_registry_version"`
	EventSchemaMajors  []string          `json:"event_schema_majors"`
	ResultSchemaMajors []string          `json:"result_schema_majors"`
	StateSchema        json.RawMessage   `json:"state_schema"`
	Adapters           []json.RawMessage `json:"adapters"`
	SupportWindows     map[string]Window `json:"support_windows"`
}

type WindowStatus struct {
	Name     string `json:"name"`
	ReviewBy string `json:"review_by"`
	Due      bool   `json:"due"`
	Expired  bool   `json:"expired"`
}

type Report struct {
	Windows []WindowStatus
}

func (r Report) Due() bool {
	for _, window := range r.Windows {
		if window.Due {
			return true
		}
	}
	return false
}

func (r Report) Expired() bool {
	for _, window := range r.Windows {
		if window.Expired {
			return true
		}
	}
	return false
}

func Load(path string) (Manifest, error) {
	if path == "" {
		return Manifest{}, errors.New("compatibility manifest path is required")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the manifest path is an explicit operator-selected input.
	if err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode compatibility manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, errors.New("compatibility manifest has trailing data")
	}
	return manifest, nil
}

// Validate checks the fixed support-window names and returns safe metadata
// for release tooling. now and warning are injected to make expiry checks
// deterministic in tests.
func Validate(manifest Manifest, now time.Time, warning time.Duration) (Report, error) {
	if manifest.SchemaVersion != "autogit.compatibility/1" {
		return Report{}, errors.New("unsupported compatibility manifest schema")
	}
	if warning < 0 {
		return Report{}, errors.New("compatibility warning window cannot be negative")
	}
	want := []string{"event_schema", "git", "github_rest", "go", "mcp", "result_schema", "sqlite"}
	got := make([]string, 0, len(manifest.SupportWindows))
	for name := range manifest.SupportWindows {
		got = append(got, name)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		return Report{}, fmt.Errorf("compatibility window count=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			return Report{}, fmt.Errorf("unexpected compatibility window %q", got[i])
		}
	}
	now = now.UTC()
	deadline := now.Add(warning)
	report := Report{Windows: make([]WindowStatus, 0, len(want))}
	for _, name := range want {
		window := manifest.SupportWindows[name]
		if window.ReviewBy == "" || (window.Minimum == "" && window.API == "" && window.Spec == "" && window.Schema == "") {
			return Report{}, fmt.Errorf("compatibility window %q is incomplete", name)
		}
		review, err := time.ParseInLocation("2006-01-02", window.ReviewBy, time.UTC)
		if err != nil {
			return Report{}, fmt.Errorf("compatibility window %q has invalid review date", name)
		}
		report.Windows = append(report.Windows, WindowStatus{Name: name, ReviewBy: window.ReviewBy, Due: !review.After(deadline), Expired: !review.After(now)})
	}
	return report, nil
}
