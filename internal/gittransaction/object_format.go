package gittransaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ObjectFormat is the Git object hash algorithm used by a repository.
type ObjectFormat string

const (
	ObjectFormatSHA1   ObjectFormat = "sha1"
	ObjectFormatSHA256 ObjectFormat = "sha256"
)

func (f ObjectFormat) ObjectIDLength() int {
	switch f {
	case ObjectFormatSHA1:
		return 40
	case ObjectFormatSHA256:
		return 64
	default:
		return 0
	}
}

func (f ObjectFormat) ValidObjectID(value string) bool {
	if len(value) != f.ObjectIDLength() {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// DetectObjectFormat asks Git for the repository's configured object format.
// It is deliberately a Git fact rather than an inference from one object ID.
func DetectObjectFormat(ctx context.Context, runner Runner, root string) (ObjectFormat, error) {
	if runner == nil || root == "" {
		return "", errors.New("object format request is invalid")
	}
	result, err := runner.Run(ctx, root, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return "", fmt.Errorf("detect Git object format: %w", err)
	}
	switch format := ObjectFormat(strings.TrimSpace(result.Output)); format {
	case ObjectFormatSHA1, ObjectFormatSHA256:
		return format, nil
	default:
		return "", fmt.Errorf("git returned unsupported object format %q", result.Output)
	}
}
