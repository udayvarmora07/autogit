package operations

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/state"
)

func TestCancelKeepsImmutableCommitIdentityAndUndoIsNotAllowedBeforeCommit(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := "operation-1"
	digest := "sha256:" + strings.Repeat("a", 64)
	if err := store.PutGitCommitIntent(context.Background(), state.GitCommitIntent{
		ID: id, RepoDir: t.TempDir(), Ref: "refs/autogit/commits/" + id, TreeOID: strings.Repeat("b", 40),
		Message: "feat: operation", CandidateDigest: digest, MessageDigest: digest, SnapshotDigest: digest,
		PolicyDigest: digest, VerifierDigest: digest, GuardDigest: digest,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Cancel(context.Background(), store, id)
	if err != nil || got.State != "CANCELLED" || got.Reason != "CANCELLED" {
		t.Fatalf("cancel=%+v err=%v", got, err)
	}
	if _, err := UndoCommitRef(context.Background(), store, got.Ref, id, "git"); err == nil {
		t.Fatal("undo accepted a cancelled operation")
	}
}
