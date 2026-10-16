package commit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

type Evidence struct{ CandidateDigest, BaseDigest, PolicyDigest, VerifierDigest string }
type Message struct{ Subject, Body, MessageDigest, CandidateDigest, BaseDigest, PolicyDigest, VerifierDigest string }

// Change is the bounded, core-owned summary used when a caller supplies task
// intent instead of a finished commit message. Content is deliberately not
// required: intent remains the semantic source and the owned candidate has
// already been independently captured and scanned by workflow.
type Change struct {
	Path      string
	Operation string
}

// Generate turns explicit task intent into a Conventional Commit subject. A
// complete Conventional Commit is preserved verbatim; otherwise a small,
// deterministic classifier supplies the type. It refuses generic stop-event
// text so a hook cannot manufacture a misleading commit from filenames alone.
func Generate(intent string, changes []Change) (string, error) {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return "", fmt.Errorf("task intent is required")
	}
	if err := Validate(intent); err == nil {
		return intent, nil
	}
	if len(changes) == 0 {
		return "", fmt.Errorf("owned change is required for generated intent")
	}
	for _, change := range changes {
		if strings.TrimSpace(change.Path) == "" || strings.TrimSpace(change.Operation) == "" {
			return "", fmt.Errorf("owned change summary is incomplete")
		}
	}
	if strings.ContainsAny(intent, "\r\n") {
		return "", fmt.Errorf("task intent must be one line")
	}
	intent = strings.Join(strings.Fields(intent), " ")
	lower := strings.ToLower(intent)
	for _, weak := range []string{"done", "complete", "task complete", "update", "update stuff", "changes", "work", "fix stuff", "implement changes"} {
		if lower == weak {
			return "", fmt.Errorf("task intent is too generic")
		}
	}
	if len([]rune(intent)) < 8 {
		return "", fmt.Errorf("task intent is too short")
	}
	typeName := "chore"
	for _, candidate := range []struct {
		name  string
		words []string
	}{
		{"fix", []string{"fix", "bug", "broken", "regression", "error"}},
		{"test", []string{"test", "verify", "coverage"}},
		{"docs", []string{"doc", "readme", "documentation"}},
		{"refactor", []string{"refactor", "rename", "restructure"}},
		{"build", []string{"build", "compile", "dependency", "dependencies"}},
		{"ci", []string{"ci", "pipeline", "workflow"}},
		{"perf", []string{"performance", "faster", "latency", "optimize"}},
		{"style", []string{"style", "format", "formatting"}},
		{"feat", []string{"add", "create", "support", "allow", "introduce"}},
	} {
		for _, word := range candidate.words {
			if containsWord(lower, word) {
				typeName = candidate.name
				break
			}
		}
		if typeName == candidate.name {
			break
		}
	}
	message := typeName + ": " + lowerFirst(intent)
	if err := Validate(message); err != nil {
		return "", fmt.Errorf("generated commit message: %w", err)
	}
	return message, nil
}

func containsWord(text, word string) bool {
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if token == word {
			return true
		}
	}
	return false
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	if runes[0] >= 'A' && runes[0] <= 'Z' {
		runes[0] += 'a' - 'A'
	}
	return string(runes)
}

func Compose(intent, message string, e Evidence) (Message, error) {
	if strings.TrimSpace(intent) == "" {
		return Message{}, fmt.Errorf("task intent is required")
	}
	if err := Validate(message); err != nil {
		return Message{}, err
	}
	for _, d := range []string{e.CandidateDigest, e.BaseDigest, e.PolicyDigest, e.VerifierDigest} {
		if !strings.HasPrefix(d, "sha256:") || len(d) != 71 {
			return Message{}, fmt.Errorf("incomplete message evidence")
		}
	}
	h := sha256.Sum256([]byte(message + "\x00" + e.CandidateDigest + "\x00" + e.BaseDigest + "\x00" + e.PolicyDigest + "\x00" + e.VerifierDigest))
	return Message{Subject: strings.Split(message, "\n")[0], Body: message, MessageDigest: "sha256:" + hex.EncodeToString(h[:]), CandidateDigest: e.CandidateDigest, BaseDigest: e.BaseDigest, PolicyDigest: e.PolicyDigest, VerifierDigest: e.VerifierDigest}, nil
}

var (
	headerRE = regexp.MustCompile(`^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)(\([A-Za-z0-9._/-]+\))?!?: .+$`)
	// provenanceTrailerRE denies every hyphenated author/review-style Git
	// trailer (Co-authored-by, Signed-off-by, Reviewed-by, Tested-by, ...).
	// TM-015 requires that untrusted completion text cannot invent authorship
	// or review provenance in permanent history, so the schema denies the
	// trailer shape rather than only the two most common spellings.
	provenanceTrailerRE = regexp.MustCompile(`(?i)^[a-z][a-z-]*-by\s*:`)
	// issueClosureRE denies GitHub/GitLab issue-closure keywords followed by
	// an issue reference: "Closes #12", "fixes: owner/repo#7", or
	// "Resolved https://github.com/owner/repo/issues/3". TM-015 lists
	// invented issue closure as a repudiation threat because the reference
	// takes effect when the commit reaches the target branch.
	issueClosureRE = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:es|ed)?|resolve[sd]?)\s*:?\s+(?:#\d+\b|[\w.-]+/[\w.-]+#\d+\b|https?://\S+/(?:issues|pull|merge_requests)/\d+\b)`)
)

func Validate(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("empty commit message")
	}
	lines := strings.Split(message, "\n")
	if len(lines[0]) > 72 {
		return fmt.Errorf("subject exceeds 72 characters")
	}
	if !headerRE.MatchString(lines[0]) {
		return fmt.Errorf("message is not Conventional Commit format")
	}
	lower := strings.ToLower(message)
	for _, line := range strings.Split(lower, "\n") {
		trimmed := strings.TrimSpace(line)
		// The lowercase copy is used for the case-insensitive checks; the
		// original line is matched for the trailer shape so the diagnosis is
		// deterministic.
		for _, bad := range []string{"gh-token", "yes public", "password", "secret"} {
			if strings.Contains(trimmed, bad) {
				return fmt.Errorf("forbidden trailer or sensitive content")
			}
		}
	}
	for _, line := range strings.Split(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if provenanceTrailerRE.MatchString(trimmed) {
			return fmt.Errorf("forbidden provenance trailer")
		}
		if issueClosureRE.MatchString(trimmed) {
			return fmt.Errorf("forbidden issue-closure reference")
		}
	}
	return nil
}
