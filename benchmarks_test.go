package autogit_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"autogit/internal/adapters"
	"autogit/internal/commit"
	"autogit/internal/events"
	"autogit/internal/lifecycle"
	"autogit/internal/policy"
	"autogit/internal/publication"
	"autogit/internal/repository"
	"autogit/internal/security"
)

func benchmarkDomainEvent(b *testing.B) []byte {
	b.Helper()
	raw, err := events.NewDomainEvent(events.DomainEventRequest{
		EventType:          "policy.set",
		OccurredAt:         "2026-09-05T00:00:00Z",
		RepoID:             "sha256:" + strings.Repeat("a", 64),
		CorrelationID:      "correlation-1",
		IdempotencyKey:     "event-1",
		ProducerInstanceID: "benchmark",
		Payload:            map[string]any{"visibility": "private"},
	})
	if err != nil {
		b.Fatal(err)
	}
	return raw
}

func benchmarkPaths(count int) []string {
	paths := make([]string, count)
	for i := range paths {
		value := strconv.Itoa(i)
		paths[i] = "pkg/file-" + strings.Repeat("0", 5-len(value)) + value + ".go"
	}
	return paths
}

func benchmarkDigest(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }

func benchmarkPublicationRequest() publication.Request {
	candidate, base, policyDigest := benchmarkDigest('a'), benchmarkDigest('b'), benchmarkDigest('c')
	return publication.Request{
		Mode: publication.ModePublic, FirstPublication: true, PublicConsent: true,
		CandidateDigest: candidate, BaseDigest: base, PolicyDigest: policyDigest,
		GuardDigest: benchmarkDigest('d'), VerifierSetDigest: benchmarkDigest('e'),
		Destination:          publication.Destination{Provider: "github", Host: "github.com", Owner: "acme", Repository: "widget", Visibility: publication.VisibilityPublic, Ref: "refs/heads/main"},
		ObservedDestination:  publication.Destination{Provider: "github", Host: "github.com", Owner: "acme", Repository: "widget", Visibility: publication.VisibilityPublic, Ref: "refs/heads/main"},
		DestinationConfirmed: true,
		Files:                []publication.FileMetadata{{Path: "README.md", Bytes: 120}, {Path: "LICENSE", Bytes: 80}, {Path: "cmd/main.go", Bytes: 200}},
		CandidateScan:        publication.ScanEvidence{Scope: publication.ScanCandidate, CandidateDigest: candidate, PolicyDigest: policyDigest, Passed: true, Digest: benchmarkDigest('f')},
		HistoryScan:          publication.ScanEvidence{Scope: publication.ScanHistory, CandidateDigest: candidate, PolicyDigest: policyDigest, Passed: true, Digest: benchmarkDigest('a')},
		Verification:         publication.VerificationEvidence{CandidateDigest: candidate, BaseDigest: base, PolicyDigest: policyDigest, GuardDigest: benchmarkDigest('d'), VerifierSetDigest: benchmarkDigest('e'), Passed: true, Required: 2, PassedCount: 2, Digest: benchmarkDigest('b')},
		License:              publication.LicenseEvidence{Selected: "MIT", FilePath: "LICENSE", Present: true},
		README:               publication.READMEInput{Path: "README.md", Content: []byte("# Widget\n\n## Usage\nRun `widget`.\n")},
		Readiness:            publication.Readiness{Tests: publication.StatusPassed, CI: publication.StatusPresent}, WorkflowSolo: true,
	}
}

func BenchmarkEventBuild(b *testing.B) {
	request := events.DomainEventRequest{EventType: "policy.set", OccurredAt: "2026-09-05T00:00:00Z", RepoID: "sha256:" + strings.Repeat("a", 64), CorrelationID: "correlation-1", IdempotencyKey: "event-1", Payload: map[string]any{"visibility": "private"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := events.NewDomainEvent(request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEventDecode(b *testing.B) {
	raw := benchmarkDomainEvent(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := events.Decode(raw, 64<<10); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAdapterCanonicalDigest(b *testing.B) {
	event := adapters.CanonicalEvent{SchemaVersion: "autogit.event/1", EventClass: "ingress", EventID: "event-1", EventType: "session.started", OccurredAt: "2026-09-05T00:00:00Z", Producer: adapters.Producer{Kind: "adapter", Adapter: "codex", Version: "1", InstallationID: "install-1", InstanceID: "instance-1"}, Scope: map[string]string{"repository_id": "repo-1"}, Ordering: adapters.Ordering{StreamID: "stream-1"}, Idempotency: adapters.Idempotency{Key: "event-1"}, Payload: map[string]any{"ok": true}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := adapters.CanonicalDigest(event); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAdapterManifest(b *testing.B) {
	adapter, err := adapters.New("codex")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = adapter.Manifest()
	}
}

func BenchmarkRepositoryDigestPaths1K(b *testing.B) {
	paths := benchmarkPaths(1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = repository.DigestPaths(paths)
	}
}

func BenchmarkRepositoryDigestPaths100K(b *testing.B) {
	paths := benchmarkPaths(100000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = repository.DigestPaths(paths)
	}
}

func BenchmarkDurableBaselineEncode(b *testing.B) {
	paths := benchmarkPaths(1000)
	files := make(map[string]repository.FileObservation, len(paths))
	for _, path := range paths {
		files[path] = repository.FileObservation{Content: []byte("package benchmark\n"), Mode: 0644, Present: true}
	}
	baseline := repository.Baseline{Paths: paths, PathsDigest: repository.DigestPaths(paths), Files: files}
	key := []byte("benchmark-identity-key")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := repository.EncodeDurableBaseline(baseline, key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommitGenerate(b *testing.B) {
	changes := []commit.Change{{Path: "internal/core.go", Operation: "modified"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := commit.Generate("Improve candidate verification", changes); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommitValidate(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := commit.Validate("feat(core): improve candidate verification"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPolicyMerge(b *testing.B) {
	base := policy.Policy{Tracking: "yes", Visibility: "private", Workflow: "safe", Provider: "github", Version: 1}
	project := policy.Policy{Visibility: "public", PublicConsent: true, PublicConsentSet: true, Version: 2}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = policy.Merge(base, project)
	}
}

func BenchmarkSecurityScan(b *testing.B) {
	snapshot := security.CandidateSnapshot{Files: make([]security.CandidateFile, 32)}
	for i := range snapshot.Files {
		snapshot.Files[i] = security.CandidateFile{Path: "pkg/file-" + strconv.Itoa(i) + ".go", Content: []byte("package benchmark\n\nfunc Example() {}\n"), Mode: 0644}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := security.ScanCandidate(context.Background(), snapshot, security.ScanOptions{})
		if !result.Safe() {
			b.Fatal("safe benchmark fixture was blocked")
		}
	}
}

func BenchmarkPublicationEvaluate(b *testing.B) {
	request := benchmarkPublicationRequest()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if report := publication.Evaluate(request); !report.Safe() {
			b.Fatalf("complete benchmark fixture was blocked: %+v", report.ReasonCodes)
		}
	}
}

func BenchmarkLifecycleReduce(b *testing.B) {
	reducer := lifecycle.NewReducer(lifecycle.Config{})
	event := lifecycle.Event{ID: "event-1", Type: lifecycle.SessionStarted, SessionID: "session-1", TaskID: "task-1", Payload: lifecycle.Payload{BaselineHead: "head-1", BaselineIndex: "index-1"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		state, result := reducer.Reduce(lifecycle.NewState("repo-1"), event)
		if result.Disposition != lifecycle.Accepted || state.Session.ID != "session-1" {
			b.Fatal("lifecycle benchmark fixture was rejected")
		}
	}
}
