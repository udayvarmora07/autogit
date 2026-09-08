package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryIsVersionedFixtureBackedAndReturnsClones(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatal(err)
	}
	registry := Registry()
	if registry.Version != RegistryVersion || len(registry.Entries) != 6 {
		t.Fatalf("registry=%+v", registry)
	}
	for _, entry := range registry.Entries {
		if entry.Adapter == "" || entry.FixtureVersion == "" || entry.PayloadFixture == "" || entry.ConfigFixture == "" {
			t.Fatalf("incomplete registry entry=%+v", entry)
		}
		fixture, err := FixtureFor(entry.Adapter)
		if err != nil {
			t.Fatal(err)
		}
		if fixture.Version != entry.FixtureVersion || fixture.PayloadDigest == "" || fixture.ConfigDigest == "" {
			t.Fatalf("fixture metadata=%+v entry=%+v", fixture, entry)
		}
		if strings.Contains(strings.ToLower(string(fixture.Payload)), "secret") || strings.Contains(strings.ToLower(string(fixture.Config)), "token") {
			t.Fatalf("fixture contains sensitive-looking data: %s", entry.Adapter)
		}
	}
	registry.Entries[0].ClientVersions[0] = "mutated"
	again := Registry()
	if again.Entries[0].ClientVersions[0] == "mutated" {
		t.Fatal("registry returned mutable internal state")
	}
}

func TestCursorFileEditPayloadUsesOfficialWorkspaceAndPathFields(t *testing.T) {
	root := t.TempDir()
	a, err := New("cursor")
	if err != nil {
		t.Fatal(err)
	}
	encodedRoot, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"hook_event_name":"afterFileEdit","conversation_id":"conversation-1","workspace_roots":[` + string(encodedRoot) + `],"file_path":"src/main.go","cursor_version":"3.14.7"}`)
	e, err := a.Translate(payload, TranslateOptions{ApprovedRoots: []string{root}, ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	if e.EventType != "files.changed" || len(e.Payload["changes"].([]map[string]any)) != 1 {
		t.Fatalf("Cursor file-edit event=%+v", e)
	}
}

func TestRegistryPayloadFixturesTranslateWithinApprovedRoot(t *testing.T) {
	root := t.TempDir()
	for _, entry := range Registry().Entries {
		t.Run(entry.Adapter, func(t *testing.T) {
			fixture, err := FixtureFor(entry.Adapter)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(fixture.Config, &config); err != nil {
				t.Fatal(err)
			}
			if entry.Config.Shape == "event-groups" {
				if _, ok := config["hooks"].(map[string]any); !ok {
					t.Fatalf("fixture config has no grouped hooks: %s", fixture.Config)
				}
			}
			encodedRoot, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			raw := bytes.ReplaceAll(fixture.Payload, []byte(`"/workspace/project"`), encodedRoot)
			a, err := New(entry.Adapter)
			if err != nil {
				t.Fatal(err)
			}
			event, err := a.Translate(raw, TranslateOptions{
				ApprovedRoots: []string{root},
				ResolvedScope: map[string]string{"repo_id": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if event.Producer.Adapter != entry.Adapter || event.Scope["repo_id"] == "" {
				t.Fatalf("translated fixture=%+v", event)
			}
		})
	}
}

func TestProbeClassifiesSupportedDegradedAndUnsupportedWithoutNetwork(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ string, _ []string) (ProbeExecution, error) {
		return ProbeExecution{Stdout: "client 2.1.233\n", ExitCode: 0}, nil
	}
	report, err := Probe(context.Background(), "claude-code", ProbeOptions{Executable: executable, Run: runner})
	if err != nil || report.Status != ProbeSupported || report.Version != "2.1.233" || report.Executable != executable {
		t.Fatalf("supported report=%+v err=%v", report, err)
	}

	runner = func(_ context.Context, _ string, _ []string) (ProbeExecution, error) {
		return ProbeExecution{Stdout: "client 99.0.0\n", ExitCode: 0}, nil
	}
	report, err = Probe(context.Background(), "claude-code", ProbeOptions{Executable: executable, Run: runner})
	if err != nil || report.Status != ProbeDegraded || report.Version != "99.0.0" {
		t.Fatalf("degraded report=%+v err=%v", report, err)
	}

	report, err = Probe(context.Background(), "opencode", ProbeOptions{Executable: executable, Run: runner})
	if err != nil || report.Status != ProbeUnsupported || !strings.Contains(report.Reason, "observation-only") {
		t.Fatalf("unsupported report=%+v err=%v", report, err)
	}

	missing, err := Probe(context.Background(), "claude-code", ProbeOptions{Executable: "definitely-not-a-client-command"})
	if err != nil || missing.Status == ProbeSupported || missing.Reason == "" {
		t.Fatalf("missing report=%+v err=%v", missing, err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("probe unexpectedly returned cancellation")
	}
}

func TestProbeDefaultTimeoutAllowsClientColdStartMargin(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := func(ctx context.Context, _ string, _ []string) (ProbeExecution, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("probe runner received no deadline")
		}
		remaining := time.Until(deadline)
		if remaining < 4*time.Second || remaining > 6*time.Second {
			t.Fatalf("default probe budget=%s, want approximately five seconds", remaining)
		}
		return ProbeExecution{Stdout: "client 2.1.233\n", ExitCode: 0}, nil
	}
	report, err := Probe(context.Background(), "claude-code", ProbeOptions{Executable: executable, Run: runner})
	if err != nil || report.Status != ProbeSupported {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestProbeRejectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Probe(ctx, "claude-code", ProbeOptions{Executable: "definitely-not-a-client-command"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context.Canceled", err)
	}
}

func TestProbeResolvesLauncherSymlinkBeforeVersionCheck(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "client")
	if err := os.WriteFile(target, []byte("portable probe fixture\n"), 0700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "cursor")
	if err := os.Symlink(target, launcher); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	expectedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Probe(context.Background(), "cursor", ProbeOptions{
		Executable: launcher,
		Run: func(_ context.Context, _ string, _ []string) (ProbeExecution, error) {
			return ProbeExecution{Stdout: "3.14.7\n", ExitCode: 0}, nil
		},
	})
	if err != nil || report.Status != ProbeSupported || report.Executable != expectedTarget {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
