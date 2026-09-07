package security

import (
	"context"
	"testing"
)

func FuzzScannerNeverPanics(f *testing.F) {
	f.Add("config.env", []byte("GH_TOKEN=secret"), uint32(0100644), false)
	f.Add("../unsafe", []byte{0, 1, 2}, uint32(0120000), true)
	f.Fuzz(func(t *testing.T, path string, content []byte, mode uint32, symlink bool) {
		if len(content) > 64<<10 {
			content = content[:64<<10]
		}
		scanner := NewPinnedOfflineScanner()
		_ = scanner.Scan(context.Background(), CandidateSnapshot{Files: []CandidateFile{{Path: path, Content: content, Mode: mode, Symlink: symlink}}})
	})
}
