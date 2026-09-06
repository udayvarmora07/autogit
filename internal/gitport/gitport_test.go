package gitport

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPushArgsAreExactSHARefAndNeverImplicit(t *testing.T) {
	args, err := PushArgs("origin", "0123456789abcdef0123456789abcdef01234567", "feature/a")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"push", "--", "origin", "0123456789abcdef0123456789abcdef01234567:refs/heads/feature/a"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for _, ref := range []string{"--all", "x:y", "../escape", "feature\nlog"} {
		if _, err := PushArgs("origin", "0123456789abcdef0123456789abcdef01234567", ref); err == nil {
			t.Errorf("ref %q accepted", ref)
		}
	}
	for _, width := range []int{39, 41, 63, 65} {
		if _, err := PushArgs("origin", strings.Repeat("a", width), "feature/a"); err == nil {
			t.Errorf("SHA width %d accepted", width)
		}
	}
}

func TestRunnerUsesArgumentArrayAndBoundsOutput(t *testing.T) {
	r := Runner{Executable: "printf", MaxOutput: 4}
	got, err := r.Run(context.Background(), ".", "hello")
	if err == nil || got.Output != "hell" || got.Truncated != true {
		t.Fatalf("result=%+v err=%v, want bounded output", got, err)
	}
}

func TestRunnerUsesHardenedGitConfigurationAndArgumentBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-specific")
	}
	script := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'global=%s\\nconfig=%s\\nprompt=%s\\nargs=%s\\n' \"$GIT_CONFIG_GLOBAL\" \"$GIT_CONFIG_NOSYSTEM\" \"$GIT_TERMINAL_PROMPT\" \"$*\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "secret-config")
	t.Setenv("SSH_COMMAND", "secret-ssh")
	got, err := (Runner{Executable: script}).Run(context.Background(), t.TempDir(), "init", "--initial-branch=main")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Output, "secret-config") || strings.Contains(got.Output, "secret-ssh") {
		t.Fatalf("ambient configuration leaked: %q", got.Output)
	}
	for _, want := range []string{"global=", "config=1", "prompt=0", "core.hooksPath=/dev/null", "core.sshCommand=", "init --initial-branch=main"} {
		if !strings.Contains(got.Output, want) {
			t.Fatalf("output=%q missing %q", got.Output, want)
		}
	}
}
