package gitport

import (
	"context"
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
