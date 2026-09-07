package install

import (
	"path/filepath"
	"testing"
)

func FuzzConfigPathScopeNeverPanics(f *testing.F) {
	f.Add("config.json", "codex")
	f.Add("../outside.json", "adapter")
	f.Add("config.json", "bad adapter")
	f.Fuzz(func(t *testing.T, relative, adapter string) {
		root := t.TempDir()
		path := filepath.Join(root, relative)
		_, _ = Plan(ConfigSpec{Adapter: adapter, Path: path, Format: FormatJSON}, []string{root})
	})
}
