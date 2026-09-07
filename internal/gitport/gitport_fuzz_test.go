package gitport

import (
	"strings"
	"testing"
)

func FuzzPushArgsNeverBuildsOptionLikeDestination(f *testing.F) {
	f.Add("origin", strings.Repeat("a", 40), "main")
	f.Add("-origin", strings.Repeat("b", 64), "-branch")
	f.Add("origin", "not-a-sha", "refs/heads/main")
	f.Fuzz(func(t *testing.T, remote, sha, ref string) {
		args, err := PushArgs(remote, sha, ref)
		if err != nil {
			return
		}
		if len(args) != 4 || args[0] != "push" || args[1] != "--" || strings.HasPrefix(args[2], "-") || strings.HasPrefix(args[3], "-") {
			t.Fatalf("unsafe push args=%q", args)
		}
	})
}
