package repository

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type baselineBenchmarkRunner struct {
	head      string
	indexPath string
	status    string
}

func (r baselineBenchmarkRunner) Run(_ context.Context, _ string, _ map[string]string, args ...string) (CommandResult, error) {
	switch strings.Join(args, "\x00") {
	case "rev-parse\x00--verify\x00--quiet\x00HEAD^{commit}":
		return CommandResult{Output: r.head + "\n"}, nil
	case "rev-parse\x00--git-path\x00index":
		return CommandResult{Output: r.indexPath + "\n"}, nil
	case "status\x00--porcelain=v1\x00-z\x00--untracked-files=all":
		return CommandResult{Output: r.status}, nil
	default:
		return CommandResult{}, os.ErrNotExist
	}
}

func BenchmarkCaptureBaseline100KDeletedPaths(b *testing.B) {
	root := b.TempDir()
	indexPath := filepath.Join(root, "index")
	if err := os.WriteFile(indexPath, []byte("benchmark-index"), 0600); err != nil {
		b.Fatal(err)
	}
	const pathCount = 100000
	var status strings.Builder
	status.Grow(pathCount * 24)
	for i := 0; i < pathCount; i++ {
		status.WriteString("D  deleted/")
		value := strconv.Itoa(i)
		status.WriteString(strings.Repeat("0", 6-len(value)))
		status.WriteString(value)
		status.WriteString(".txt\x00")
	}
	runner := baselineBenchmarkRunner{
		head:      strings.Repeat("a", 40),
		indexPath: indexPath,
		status:    status.String(),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		baseline, err := CaptureBaseline(context.Background(), runner, root)
		if err != nil {
			b.Fatal(err)
		}
		if len(baseline.Paths) != pathCount || len(baseline.Files) != pathCount {
			b.Fatalf("baseline paths=%d files=%d, want %d", len(baseline.Paths), len(baseline.Files), pathCount)
		}
	}
}
