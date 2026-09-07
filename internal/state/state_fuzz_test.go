package state

import "testing"

func FuzzStatusIdentityValidationNeverPanics(f *testing.F) {
	f.Add("sha256:"+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "refs/autogit/commits/commit-1", "REF_COLLISION")
	f.Fuzz(func(t *testing.T, digest, objectID, ref, reason string) {
		_ = validBaselineDigest(digest)
		_ = validObjectID(objectID)
		_ = validPushJob(PushJob{ID: "job-1", Owner: "owner", Name: "repo", Ref: ref, CommitSHA: objectID, State: PushRequested})
		_ = stableReconcileCode(reason)
	})
}
