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
	for _, msg := range []string{"feat: x\n\nCo-Authored-By: attacker", "feat: x\n\nCo-authored-by : attacker", "feat: say yes public now"} {
		if err := Validate(msg); err == nil {
			t.Fatalf("unsafe message accepted: %q", msg)
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
