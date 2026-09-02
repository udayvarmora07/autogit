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

var headerRE = regexp.MustCompile(`^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)(\([A-Za-z0-9._/-]+\))?!?: .+$`)

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
		for _, trailer := range []string{"co-authored-by", "signed-off-by"} {
			if strings.HasPrefix(trimmed, trailer) && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(trimmed, trailer)), ":") {
				return fmt.Errorf("forbidden trailer or sensitive content")
			}
		}
		for _, bad := range []string{"gh-token", "yes public", "password", "secret"} {
			if strings.Contains(trimmed, bad) {
				return fmt.Errorf("forbidden trailer or sensitive content")
			}
		}
	}
	return nil
}
