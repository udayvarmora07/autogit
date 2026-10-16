package commit

import (
	"strings"
	"testing"
)

func TestValidateConventionalCommitAndSubjectLimit(t *testing.T) {
	if err := Validate("feat(parser): accept events"); err != nil {
		t.Fatal(err)
	}
	if err := Validate("update stuff"); err == nil {
		t.Fatal("generic message accepted")
	}
	if err := Validate("feat: " + "1234567890123456789012345678901234567890123456789012345678901234567890"); err == nil {
		t.Fatal("long subject accepted")
	}
}

func TestValidateRejectsForbiddenTrailersAndPromptText(t *testing.T) {
	for _, msg := range []string{
		"feat: x\n\nCo-Authored-By: attacker",
		"feat: x\n\nCo-authored-by : attacker",
		"feat: say yes public now",
		"feat: x\n\nReviewed-by: attacker <a@example.com>",
		"feat: x\n\nAcked-By : attacker",
		"feat: x\n\nTested-by: someone",
		"fix: x\n\nCloses #42",
		"fix: x\n\nFixes: owner/repo#7",
		"fix: x\n\nResolved https://github.com/owner/repo/issues/3",
		"fix: closes #12 while touching the parser",
	} {
		if err := Validate(msg); err == nil {
			t.Fatalf("unsafe message accepted: %q", msg)
		}
	}
}

// Keywords in prose without an issue reference are not closure references and
// must keep working, so the denylist does not reject ordinary descriptions.
func TestValidateAcceptsClosureKeywordsWithoutIssueReference(t *testing.T) {
	for _, msg := range []string{
		"fix: resolve the flaky parser test",
		"feat: close the gap between parsers",
		"test: cover fixes for quoted paths",
		"docs: describe how issues are referenced in messages",
	} {
		if err := Validate(msg); err != nil {
			t.Fatalf("prose message rejected: %q: %v", msg, err)
		}
	}
}

func TestComposeBindsMessageEvidenceToCandidateAndPolicy(t *testing.T) {
	e := Evidence{CandidateDigest: "sha256:" + strings.Repeat("a", 64), BaseDigest: "sha256:" + strings.Repeat("b", 64), PolicyDigest: "sha256:" + strings.Repeat("c", 64), VerifierDigest: "sha256:" + strings.Repeat("d", 64)}
	m, err := Compose("parser accepts events", "feat(parser): accept events", e)
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageDigest == "" || m.CandidateDigest != e.CandidateDigest || m.PolicyDigest != e.PolicyDigest {
		t.Fatalf("message evidence=%+v", m)
	}
}

func TestGenerateBuildsConventionalMessageFromMeaningfulIntent(t *testing.T) {
	message, err := Generate("fix parser accepts quoted paths", []Change{
		{Path: "internal/parser.go", Operation: "modified"},
		{Path: "internal/parser_test.go", Operation: "added"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if message != "fix: fix parser accepts quoted paths" {
		t.Fatalf("message=%q", message)
	}
	if err := Validate(message); err != nil {
		t.Fatalf("generated message is invalid: %v", err)
	}
}

func TestGeneratePreservesExplicitConventionalMessageAndRejectsWeakIntent(t *testing.T) {
	explicit := "feat(parser): accept quoted paths"
	got, err := Generate(explicit, nil)
	if err != nil || got != explicit {
		t.Fatalf("got=%q err=%v", got, err)
	}
	for _, intent := range []string{"", "update stuff", "done", "task complete", "feat: secret password"} {
		if _, err := Generate(intent, nil); err == nil {
			t.Fatalf("weak intent %q accepted", intent)
		}
	}
	if _, err := Generate("fix parser accepts paths", nil); err == nil {
		t.Fatal("generated message accepted without an owned change")
	}
}
