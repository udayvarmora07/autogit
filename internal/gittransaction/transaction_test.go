package gittransaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"autogit/internal/verification"
)

func TestCreateUsesIsolatedIndexAndOnlyAutoGitRef(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "owned name;$.txt"), "owned\n")
	git(t, repo, "add", "--", "owned name;$.txt")
	git(t, repo, "commit", "-m", "chore: baseline")

	writeFile(t, filepath.Join(repo, "owned name;$.txt"), "candidate\n")
	writeFile(t, filepath.Join(repo, "unowned.txt"), "leave me\n")
	git(t, repo, "add", "--", "unowned.txt")
	indexBefore := mustRead(t, filepath.Join(repo, ".git", "index"))

	store := &memoryIntentStore{}
	tx := New(SystemRunner{}, store)
	got, err := tx.Create(context.Background(), Request{
		ID:           "job-special-1",
		RepoDir:      repo,
		Snapshot:     []SnapshotEntry{snapshotFile(t, filepath.Join(repo, "owned name;$.txt"), "candidate\n")},
		Message:      "feat: capture owned file",
		PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA == "" || got.TreeOID == "" || got.Ref != "refs/autogit/commits/job-special-1" {
		t.Fatalf("result=%+v", got)
	}
	if string(indexBefore) != string(mustRead(t, filepath.Join(repo, ".git", "index"))) {
		t.Fatal("user index changed")
	}
	if gotRef := git(t, repo, "rev-parse", "--verify", got.Ref); gotRef != got.SHA {
		t.Fatalf("ref=%s sha=%s", gotRef, got.SHA)
	}
	if head := git(t, repo, "rev-parse", "HEAD"); head == got.SHA {
		t.Fatal("current branch was updated")
	}
	if tree := git(t, repo, "show", "--format=%T", "--no-patch", got.SHA); tree != got.TreeOID {
		t.Fatalf("tree=%s want=%s", tree, got.TreeOID)
	}
	if entries := git(t, repo, "ls-tree", "--name-only", got.SHA); entries != "owned name;$.txt" {
		t.Fatalf("candidate included unowned index entry: %q", entries)
	}
	if got.State != "CREATED" || len(store.intents) != 1 || len(store.commits) != 1 {
		t.Fatalf("store=%+v", store)
	}
}

func TestPrepareBuildsCandidateWithoutDurableOrUserStateMutation(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "owned.txt"), "base\n")
	git(t, repo, "add", "--", "owned.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	writeFile(t, filepath.Join(repo, "unrelated.txt"), "staged user work\n")
	git(t, repo, "add", "--", "unrelated.txt")

	store := &memoryIntentStore{}
	tx := New(SystemRunner{}, store)
	indexBefore := mustRead(t, filepath.Join(repo, ".git", "index"))
	refsBefore := showRefs(t, repo)
	headBefore := git(t, repo, "rev-parse", "HEAD")
	prepared, err := tx.Prepare(context.Background(), Request{
		ID: "prepare-only", RepoDir: repo,
		Snapshot: []SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}},
		Message:  "feat: prepare only", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil || prepared.TreeOID() == "" || prepared.CandidateDigest() == "" || prepared.ParentSHA() != headBefore {
		t.Fatalf("prepared candidate=%+v", prepared)
	}
	if want := digest([]byte("git-tree\x00" + prepared.TreeOID())); prepared.CandidateDigest() != want {
		t.Fatalf("candidate digest=%q want=%q", prepared.CandidateDigest(), want)
	}
	if got := git(t, repo, "ls-tree", "--name-only", prepared.TreeOID()); got != "owned.txt" {
		t.Fatalf("candidate tree entries=%q", got)
	}
	if len(store.intents) != 0 || refsBefore != showRefs(t, repo) || headBefore != git(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("Prepare mutated durable Git state: intents=%d refs=%q", len(store.intents), showRefs(t, repo))
	}
	if string(indexBefore) != string(mustRead(t, filepath.Join(repo, ".git", "index"))) {
		t.Fatal("Prepare changed user index")
	}
}

func TestCommitPreparedCommitsExactlyCandidateAndPreservesUserState(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "owned.txt"), "base\n")
	git(t, repo, "add", "--", "owned.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	writeFile(t, filepath.Join(repo, "unrelated.txt"), "staged user work\n")
	git(t, repo, "add", "--", "unrelated.txt")
	indexBefore := mustRead(t, filepath.Join(repo, ".git", "index"))
	headBefore := git(t, repo, "rev-parse", "HEAD")
	store := &memoryIntentStore{}
	tx := New(SystemRunner{}, store)
	prepared, err := tx.Prepare(context.Background(), Request{
		ID: "commit-prepared", RepoDir: repo,
		Snapshot: []SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}},
		Message:  "feat: commit prepared", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tx.CommitPrepared(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if got.TreeOID != prepared.TreeOID() || got.CandidateDigest != prepared.CandidateDigest() {
		t.Fatalf("commit=%+v prepared tree=%s digest=%s", got, prepared.TreeOID(), prepared.CandidateDigest())
	}
	if ref := git(t, repo, "rev-parse", "--verify", "refs/autogit/commits/commit-prepared"); ref != got.SHA {
		t.Fatalf("AutoGit ref=%s commit=%s", ref, got.SHA)
	}
	if tree := git(t, repo, "show", "--format=%T", "--no-patch", got.SHA); tree != prepared.TreeOID() {
		t.Fatalf("committed tree=%s prepared=%s", tree, prepared.TreeOID())
	}
	if content := git(t, repo, "show", got.SHA+":owned.txt"); content != "candidate" {
		t.Fatalf("committed candidate content=%q", content)
	}
	if gotHead := git(t, repo, "rev-parse", "HEAD"); gotHead != headBefore {
		t.Fatalf("current branch moved from %s to %s", headBefore, gotHead)
	}
	if string(indexBefore) != string(mustRead(t, filepath.Join(repo, ".git", "index"))) {
		t.Fatal("CommitPrepared changed user index")
	}
}

func TestCommitVerifiedCommitsOnlyWithRegistryProducedEvidence(t *testing.T) {
	repo := newVerifiedCommitRepo(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := verification.NewVerifierRegistry([]verification.TrustedVerifierSpec{{Name: "trusted", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	policy := verification.VerificationPolicy{Visibility: "public"}
	request := Request{ID: "verified-commit", RepoDir: repo,
		Snapshot: []SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}},
		Message:  "feat: verified commit", PolicyDigest: digest([]byte("policy")), VerifierDigest: registry.VerifierSetDigest, GuardDigest: digest([]byte("guard"))}
	store := &memoryIntentStore{}
	tx := New(SystemRunner{}, store)
	prepared, err := tx.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	trustedRequest := verification.TrustedRequest{CandidateDigest: prepared.CandidateDigest(), BaseDigest: trustedBaseDigest(prepared.BaseSHA()), PolicyDigest: prepared.PolicyDigest(), GuardDigest: prepared.GuardDigest(), Dir: repo}
	result, err := registry.Verify(context.Background(), policy, trustedRequest, &commitVerificationRunner{})
	if err != nil || !result.ValidFor(trustedRequest, policy, registry) {
		t.Fatalf("registry evidence invalid: result=%#v err=%v", result, err)
	}
	indexBefore := mustRead(t, filepath.Join(repo, ".git", "index"))
	headBefore := git(t, repo, "rev-parse", "HEAD")
	refsBefore := showRefs(t, repo)
	got, err := tx.CommitVerified(context.Background(), prepared, result, policy, registry)
	if err != nil {
		t.Fatal(err)
	}
	if got.TreeOID != prepared.TreeOID() || git(t, repo, "show", "--format=%T", "--no-patch", got.SHA) != prepared.TreeOID() {
		t.Fatalf("commit tree=%s prepared=%s", got.TreeOID, prepared.TreeOID())
	}
	if git(t, repo, "show", got.SHA+":owned.txt") != "candidate" {
		t.Fatal("committed tree did not contain exact candidate")
	}
	if git(t, repo, "rev-parse", "HEAD") != headBefore || string(indexBefore) != string(mustRead(t, filepath.Join(repo, ".git", "index"))) {
		t.Fatal("verified commit changed user HEAD or index")
	}
	if len(store.intents) != 1 || len(store.commits) != 1 || showRefs(t, repo) != got.SHA+" refs/autogit/commits/verified-commit\n"+refsBefore {
		t.Fatalf("durable/ref state changed unexpectedly: intents=%d commits=%d refs=%q before=%q", len(store.intents), len(store.commits), showRefs(t, repo), refsBefore)
	}
}

func TestCommitVerifiedRejectsEveryEvidenceBindingBeforeDurableMutation(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*verification.VerificationResult)
	}{
		{name: "candidate binding", mutate: func(r *verification.VerificationResult) {
			r.Evidence[0].CandidateDigest = digest([]byte("other-candidate"))
		}},
		{name: "base binding", mutate: func(r *verification.VerificationResult) { r.Evidence[0].BaseDigest = digest([]byte("other-base")) }},
		{name: "policy binding", mutate: func(r *verification.VerificationResult) { r.Evidence[0].PolicyDigest = digest([]byte("other-policy")) }},
		{name: "guard binding", mutate: func(r *verification.VerificationResult) { r.Evidence[0].GuardDigest = digest([]byte("other-guard")) }},
		{name: "tampered evidence", mutate: func(r *verification.VerificationResult) { r.Evidence[0].EvidenceDigest = digest([]byte("tampered")) }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			repo, prepared, result, policy, registry := verifiedPreparedAndEvidence(t, "reject-"+strings.ReplaceAll(strings.ToLower(tc.name), " ", "-"))
			tc.mutate(&result)
			store := &memoryIntentStore{}
			tx := New(SystemRunner{}, store)
			refsBefore := showRefs(t, repo)
			if _, err := tx.CommitVerified(context.Background(), prepared, result, policy, registry); err == nil {
				t.Fatal("tampered verification evidence accepted")
			}
			if len(store.intents) != 0 || len(store.commits) != 0 || refsBefore != showRefs(t, repo) {
				t.Fatalf("rejected evidence mutated durable state: intents=%d commits=%d refs=%q", len(store.intents), len(store.commits), showRefs(t, repo))
			}
		})
	}
	// This catches a gate that validates only evidence fields and forgets the
	// registry-produced verifier-set binding.
	repo, prepared, result, policy, registry := verifiedPreparedAndEvidence(t, "reject-verifier-set")
	result.VerifierSetDigest = digest([]byte("other-verifier-set"))
	store := &memoryIntentStore{}
	refsBefore := showRefs(t, repo)
	if _, err := New(SystemRunner{}, store).CommitVerified(context.Background(), prepared, result, policy, registry); err == nil {
		t.Fatal("tampered verifier-set evidence accepted")
	}
	if len(store.intents) != 0 || showRefs(t, repo) != refsBefore {
		t.Fatal("tampered verifier-set evidence mutated durable state")
	}
}

func TestCommitVerifiedRejectsNoVerifierDecisionAndPreparedVerifierMismatch(t *testing.T) {
	repo := newVerifiedCommitRepo(t)
	policy := verification.VerificationPolicy{Visibility: "public"}
	noVerifierRegistry, err := verification.NewVerifierRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ID: "reject-no-verifier", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}}, Message: "feat: reject no verifier", PolicyDigest: digest([]byte("policy")), VerifierDigest: noVerifierRegistry.VerifierSetDigest, GuardDigest: digest([]byte("guard"))}
	store := &memoryIntentStore{}
	tx := New(SystemRunner{}, store)
	prepared, err := tx.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	trustedRequest := verification.TrustedRequest{CandidateDigest: prepared.CandidateDigest(), BaseDigest: trustedBaseDigest(prepared.BaseSHA()), PolicyDigest: prepared.PolicyDigest(), GuardDigest: prepared.GuardDigest(), Dir: repo}
	noVerifier, err := noVerifierRegistry.Verify(context.Background(), policy, trustedRequest, &commitVerificationRunner{})
	if err != nil || noVerifier.Decision != verification.DecisionNoVerifier {
		t.Fatalf("no-verifier result=%#v err=%v", noVerifier, err)
	}
	refsBefore := showRefs(t, repo)
	if _, err := tx.CommitVerified(context.Background(), prepared, noVerifier, policy, noVerifierRegistry); err == nil {
		t.Fatal("no-verifier decision accepted")
	}
	if len(store.intents) != 0 || showRefs(t, repo) != refsBefore {
		t.Fatal("no-verifier decision mutated durable state")
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := verification.NewVerifierRegistry([]verification.TrustedVerifierSpec{{Name: "trusted", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = tx.Prepare(context.Background(), Request{ID: "reject-mismatched-set", RepoDir: repo, Snapshot: request.Snapshot, Message: "feat: reject mismatch", PolicyDigest: request.PolicyDigest, VerifierDigest: emptyDigest(), GuardDigest: request.GuardDigest})
	if err != nil {
		t.Fatal(err)
	}
	trustedRequest = verification.TrustedRequest{CandidateDigest: prepared.CandidateDigest(), BaseDigest: trustedBaseDigest(prepared.BaseSHA()), PolicyDigest: prepared.PolicyDigest(), GuardDigest: prepared.GuardDigest(), Dir: repo}
	result, err := registry.Verify(context.Background(), policy, trustedRequest, &commitVerificationRunner{})
	if err != nil {
		t.Fatal(err)
	}
	refsBefore = showRefs(t, repo)
	if _, err := tx.CommitVerified(context.Background(), prepared, result, policy, registry); err == nil {
		t.Fatal("Prepared verifier digest mismatch accepted")
	}
	if len(store.intents) != 0 || showRefs(t, repo) != refsBefore {
		t.Fatal("Prepared verifier mismatch mutated durable state")
	}
}

func verifiedPreparedAndEvidence(t *testing.T, id string) (string, *Prepared, verification.VerificationResult, verification.VerificationPolicy, *verification.VerifierRegistry) {
	t.Helper()
	repo := newVerifiedCommitRepo(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := verification.NewVerifierRegistry([]verification.TrustedVerifierSpec{{Name: "trusted", Version: "1", Argv: []string{exe}, Applicable: true}})
	if err != nil {
		t.Fatal(err)
	}
	policy := verification.VerificationPolicy{Visibility: "public"}
	prepared, err := New(SystemRunner{}, &memoryIntentStore{}).Prepare(context.Background(), Request{ID: id, RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}}, Message: "feat: evidence gate", PolicyDigest: digest([]byte("policy")), VerifierDigest: registry.VerifierSetDigest, GuardDigest: digest([]byte("guard"))})
	if err != nil {
		t.Fatal(err)
	}
	req := verification.TrustedRequest{CandidateDigest: prepared.CandidateDigest(), BaseDigest: trustedBaseDigest(prepared.BaseSHA()), PolicyDigest: prepared.PolicyDigest(), GuardDigest: prepared.GuardDigest(), Dir: repo}
	result, err := registry.Verify(context.Background(), policy, req, &commitVerificationRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ValidFor(req, policy, registry) {
		t.Fatal("test fixture produced invalid registry evidence")
	}
	return repo, prepared, result, policy, registry
}

func newVerifiedCommitRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "owned.txt"), "base\n")
	git(t, repo, "add", "--", "owned.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	return repo
}

func trustedBaseDigest(parent string) string { return digest([]byte(parent)) }

type commitVerificationRunner struct{}

func (*commitVerificationRunner) Run(_ context.Context, _ string, _ map[string]string, _ ...string) (verification.Result, error) {
	return verification.Result{ExitCode: 0}, nil
}

func TestCommitPreparedRejectsHEADOrIndexChangeBeforeDurableMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "HEAD", mutate: func(t *testing.T, repo string) {
			git(t, repo, "commit", "--allow-empty", "-m", "chore: concurrent head")
		}},
		{name: "index", mutate: func(t *testing.T, repo string) {
			writeFile(t, filepath.Join(repo, "unrelated.txt"), "concurrent staged work\n")
			git(t, repo, "add", "--", "unrelated.txt")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			git(t, repo, "config", "user.name", "AutoGit Test")
			git(t, repo, "config", "user.email", "autogit@example.test")
			writeFile(t, filepath.Join(repo, "owned.txt"), "base\n")
			git(t, repo, "add", "--", "owned.txt")
			git(t, repo, "commit", "-m", "chore: baseline")
			store := &memoryIntentStore{}
			tx := New(SystemRunner{}, store)
			prepared, err := tx.Prepare(context.Background(), Request{
				ID: "prepared-race-" + strings.ToLower(tc.name), RepoDir: repo,
				Snapshot: []SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}},
				Message:  "feat: prepared race", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
			})
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, repo)
			refsBefore := showRefs(t, repo)
			if _, err := tx.CommitPrepared(context.Background(), prepared); err == nil {
				t.Fatal("CommitPrepared accepted changed repository state")
			}
			if len(store.intents) != 0 || refsBefore != showRefs(t, repo) {
				t.Fatalf("changed state caused durable mutation: intents=%d refs=%q", len(store.intents), showRefs(t, repo))
			}
		})
	}
}

func TestCreateRejectsUnsafeInputWithoutIntentOrGitMutation(t *testing.T) {
	repo := newRepo(t)
	before := showRefs(t, repo)
	store := &memoryIntentStore{}
	_, err := New(SystemRunner{}, store).Create(context.Background(), Request{
		ID:           "bad/../../ref",
		RepoDir:      repo,
		Snapshot:     []SnapshotEntry{{Path: "../outside", Content: []byte("x\n")}},
		Message:      "feat: unsafe",
		PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err == nil {
		t.Fatal("unsafe request accepted")
	}
	if len(store.intents) != 0 || before != showRefs(t, repo) {
		t.Fatalf("validation mutated state: intents=%d", len(store.intents))
	}
}

func TestCreateRejectsUnprovenEvidenceBeforeGitMutation(t *testing.T) {
	repo := newRepo(t)
	store := &memoryIntentStore{}
	_, err := New(SystemRunner{}, store).Create(context.Background(), Request{
		ID: "evidence-1", RepoDir: repo,
		Snapshot: []SnapshotEntry{{Path: "a.txt", Content: []byte("a\n"), Mode: 0644}},
		Message:  "feat: evidence", PolicyDigest: "policy", VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err == nil {
		t.Fatal("invalid evidence accepted")
	}
	if len(store.intents) != 0 || showRefs(t, repo) != "" {
		t.Fatalf("invalid evidence mutated state: intents=%d refs=%q", len(store.intents), showRefs(t, repo))
	}
}

func TestSystemRunnerBoundsOutputAndHonorsCancellation(t *testing.T) {
	r := SystemRunner{Executable: "printf", MaxOutput: 4}
	got, err := r.Run(context.Background(), t.TempDir(), nil, "hello")
	if err == nil || got.Output != "hell" || !got.Truncated {
		t.Fatalf("bounded result=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SystemRunner{Executable: "printf"}).Run(ctx, t.TempDir(), nil, "hello"); err == nil {
		t.Fatal("cancelled command succeeded")
	}
}

func TestCommitTreeUsesExplicitAutoGitIdentityInsteadOfRepositoryConfig(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "Attacker")
	git(t, repo, "config", "user.email", "attacker@example.test")
	writeFile(t, filepath.Join(repo, "owned.txt"), "candidate\n")
	db := &memoryIntentStore{}
	got, err := New(SystemRunner{}, db).Create(context.Background(), Request{
		ID: "explicit-identity", RepoDir: repo,
		Snapshot: []SnapshotEntry{{Path: "owned.txt", Content: []byte("candidate\n"), Mode: 0644}},
		Message:  "feat: explicit identity", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	author := git(t, repo, "show", "-s", "--format=%an <%ae>", got.SHA)
	if author != "AutoGit <autogit@localhost>" {
		t.Fatalf("commit author=%q, want explicit AutoGit identity", author)
	}
}

func TestValidateIntentRejectsNonCanonicalRepositoryAndMessage(t *testing.T) {
	base := testPersistentIntent("validate")
	for name, mutate := range map[string]func(*Intent){
		"non-canonical repository": func(i *Intent) { i.RepoDir = "/repo/../repo" },
		"non-conventional message": func(i *Intent) { i.Message = "not conventional\n" },
		"empty message":            func(i *Intent) { i.Message = "" },
	} {
		candidate := base
		mutate(&candidate)
		if err := validateIntent(candidate); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestRecoverInspectsExactEvidenceAndNeverRepeatsCommit(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	writeFile(t, filepath.Join(repo, "a.txt"), "b\n")

	store := &memoryIntentStore{}
	runner := SystemRunner{}
	request := Request{ID: "recover-1", RepoDir: repo, Snapshot: []SnapshotEntry{snapshotFile(t, filepath.Join(repo, "a.txt"), "b\n")}, Message: "feat: recover", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}
	// Simulate a crash after intent and commit object creation but before ref update.
	parent := git(t, repo, "rev-parse", "HEAD")
	index := filepath.Join(t.TempDir(), "index")
	env := map[string]string{"GIT_INDEX_FILE": index}
	if _, err := runner.Run(context.Background(), repo, env, "read-tree", "--reset", parent); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), repo, env, "add", "--", "a.txt"); err != nil {
		t.Fatal(err)
	}
	tree := strings.TrimSpace(mustRun(t, runner, repo, env, "write-tree"))
	message := canonicalMessage(request.Message)
	messagePath := filepath.Join(t.TempDir(), "message")
	writeFile(t, messagePath, message)
	commitOutput := mustRun(t, runner, repo, env, "commit-tree", tree, "-p", parent, "-F", messagePath)
	expectedSHA := strings.TrimSpace(commitOutput)
	preparedIntent := Intent{ID: request.ID, RepoDir: repo, Ref: refFor(request.ID, ""), ParentSHA: parent, TreeOID: tree,
		Message: message, CandidateDigest: treeDigest(tree), MessageDigest: messageDigest(message), SnapshotDigest: snapshotDigest(request.Snapshot), PolicyDigest: request.PolicyDigest, VerifierDigest: request.VerifierDigest, GuardDigest: request.GuardDigest}
	if err := store.PutCommitIntent(context.Background(), preparedIntent); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), repo, nil, "update-ref", preparedIntent.Ref, expectedSHA, ""); err != nil {
		t.Fatal(err)
	}
	got, err := New(runner, store).Recover(context.Background(), request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA != expectedSHA || store.commits[request.ID] != expectedSHA {
		t.Fatalf("recovery=%+v commits=%+v", got, store.commits)
	}
	if _, err := New(runner, store).Recover(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.commits) != 1 {
		t.Fatal("recovery created duplicate result")
	}
}

func TestCreateMarksUnknownRefUpdateAndRecoveryProvesIt(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	runner := &failAfterRefRunner{base: SystemRunner{}}
	store := &memoryIntentStore{}
	req := Request{ID: "ref-crash", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "a.txt", Content: []byte("b\n"), Mode: 0644}}, Message: "feat: ref crash", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}
	if _, err := New(runner, store).Create(context.Background(), req); err == nil {
		t.Fatal("unknown ref outcome reported as success")
	}
	if store.reconciled[req.ID] == "" {
		t.Fatal("unknown ref outcome was not marked for reconciliation")
	}
	got, err := New(runner, store).Recover(context.Background(), req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA == "" || store.commits[req.ID] != got.SHA {
		t.Fatalf("recovery=%+v commits=%+v", got, store.commits)
	}
}

func TestCreateReusesExistingIntentAndDoesNotCreateAnotherCommit(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	writeFile(t, filepath.Join(repo, "a.txt"), "b\n")
	store := &memoryIntentStore{}
	runner := &countingRunner{base: SystemRunner{}}
	tx := New(runner, store)
	req := Request{ID: "same-job", RepoDir: repo, Snapshot: []SnapshotEntry{snapshotFile(t, filepath.Join(repo, "a.txt"), "b\n")}, Message: "feat: one commit", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}
	first, err := tx.Create(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tx.Create(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA != second.SHA || len(store.commits) != 1 || runner.commitTrees != 1 {
		t.Fatalf("first=%+v second=%+v commits=%d commit-trees=%d", first, second, len(store.commits), runner.commitTrees)
	}
}

func TestCreateRejectsSameIDWithDifferentSnapshot(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	runner := &countingRunner{base: SystemRunner{}}
	store := &memoryIntentStore{}
	tx := New(runner, store)
	firstReq := Request{ID: "collision-1", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "a.txt", Content: []byte("one\n"), Mode: 0644}}, Message: "feat: collision", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}
	if _, err := tx.Create(context.Background(), firstReq); err != nil {
		t.Fatal(err)
	}
	secondReq := firstReq
	secondReq.Snapshot = []SnapshotEntry{{Path: "a.txt", Content: []byte("two\n"), Mode: 0644}}
	if _, err := tx.Create(context.Background(), secondReq); err == nil {
		t.Fatal("same ID with different snapshot accepted")
	}
	if runner.commitTrees != 1 {
		t.Fatalf("commit-tree calls=%d", runner.commitTrees)
	}
}

func TestCreateSupportsEmptyRepository(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "first file.txt"), "first\n")
	store := &memoryIntentStore{}
	got, err := New(SystemRunner{}, store).Create(context.Background(), Request{ID: "empty-1", RepoDir: repo, Snapshot: []SnapshotEntry{snapshotFile(t, filepath.Join(repo, "first file.txt"), "first\n")}, Message: "feat: initial file", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()})
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentSHA != "" || git(t, repo, "rev-parse", "--verify", got.Ref) != got.SHA {
		t.Fatalf("initial result=%+v", got)
	}
	if out := git(t, repo, "ls-tree", "--name-only", got.SHA); out != "first file.txt" {
		t.Fatalf("tree entries=%q", out)
	}
}

func TestCreateStagesDeletionRenameAndModeFromExplicitPaths(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "old.txt"), "rename me\n")
	git(t, repo, "add", "--", "old.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	if err := os.Rename(filepath.Join(repo, "old.txt"), filepath.Join(repo, "new name.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo, "new name.txt"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := New(SystemRunner{}, &memoryIntentStore{}).Create(context.Background(), Request{ID: "rename-1", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "old.txt", Delete: true}, snapshotFile(t, filepath.Join(repo, "new name.txt"), "rename me\n")}, Message: "refactor: rename file", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()})
	if err != nil {
		t.Fatal(err)
	}
	entries := git(t, repo, "ls-tree", "--name-only", got.SHA)
	if strings.Contains(entries, "old.txt") || entries != "new name.txt" {
		t.Fatalf("rename tree=%q", entries)
	}
	wantMode := "100755 blob "
	if runtime.GOOS == "windows" {
		// Windows git has no executable bit: files land in the tree as 100644.
		wantMode = "100644 blob "
	}
	if mode := git(t, repo, "ls-tree", got.SHA, "--", "new name.txt"); !strings.HasPrefix(mode, wantMode) {
		t.Fatalf("mode entry=%q", mode)
	}
}

func TestCreateRejectsSymlinkWithoutPersistingIntent(t *testing.T) {
	repo := newRepo(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	writeFile(t, outside, "secret\n")
	link := filepath.Join(repo, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	store := &memoryIntentStore{}
	if _, err := New(SystemRunner{}, store).Create(context.Background(), Request{ID: "symlink-1", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "link.txt", Content: []byte("not link\n")}}, Message: "feat: unsafe link", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}); err == nil {
		t.Fatal("symlink accepted")
	}
	if len(store.intents) != 0 || showRefs(t, repo) != "" {
		t.Fatalf("symlink caused mutation: intents=%d refs=%q", len(store.intents), showRefs(t, repo))
	}
}

func TestCreateReconcilesWhenOwnedWorktreeChangesConcurrently(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	writeFile(t, filepath.Join(repo, "a.txt"), "candidate\n")
	store := &memoryIntentStore{}
	runner := &editAfterTreeRunner{base: SystemRunner{}, path: filepath.Join(repo, "a.txt")}
	got, err := New(runner, store).Create(context.Background(), Request{ID: "race-1", RepoDir: repo, Snapshot: []SnapshotEntry{snapshotFile(t, filepath.Join(repo, "a.txt"), "candidate\n")}, Message: "feat: race", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()})
	if err != nil {
		t.Fatal("immutable snapshot was rejected", err)
	}
	if gotTree := git(t, repo, "show", "-s", "--format=%T", got.SHA); gotTree != got.TreeOID {
		t.Fatalf("tree=%s want=%s", gotTree, got.TreeOID)
	}
	if content := git(t, repo, "show", got.SHA+":a.txt"); content != "candidate" {
		t.Fatalf("candidate tree content=%q", content)
	}
}

func TestCreateUsesExactSnapshotWithoutCleanFilterOrHook(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "base\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	marker := filepath.Join(t.TempDir(), "filter-ran")
	git(t, repo, "config", "filter.hostile.clean", "sh -c 'echo ran > "+marker+"; tr a-z A-Z'")
	git(t, repo, "config", "filter.hostile.smudge", "cat")
	git(t, repo, "config", "filter.hostile.required", "true")
	git(t, repo, "config", "filter.hostile.process", "false")
	writeFile(t, filepath.Join(repo, "a.txt"), "mutable worktree\n")
	store := &memoryIntentStore{}
	got, err := New(SystemRunner{}, store).Create(context.Background(), Request{ID: "filter-1", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "a.txt", Content: []byte("exact snapshot\n"), Mode: 0644}}, Message: "feat: exact bytes", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()})
	if err != nil {
		t.Fatal(err)
	}
	if content := git(t, repo, "show", got.SHA+":a.txt"); content != "exact snapshot" {
		t.Fatalf("tree content=%q", content)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("hostile clean filter ran: %v", err)
	}
}

func TestCreateRejectsConcurrentUserIndexChangeBeforeRef(t *testing.T) {
	repo := newRepo(t)
	git(t, repo, "config", "user.name", "AutoGit Test")
	git(t, repo, "config", "user.email", "autogit@example.test")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	git(t, repo, "add", "--", "a.txt")
	git(t, repo, "commit", "-m", "chore: baseline")
	writeFile(t, filepath.Join(repo, "a.txt"), "candidate\n")
	store := &memoryIntentStore{}
	runner := &indexEditRunner{base: SystemRunner{}, repo: repo}
	if _, err := New(runner, store).Create(context.Background(), Request{ID: "index-race", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "a.txt", Content: []byte("candidate\n"), Mode: 0644}}, Message: "feat: index race", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}); err == nil {
		t.Fatal("concurrent index edit accepted")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "refs", "autogit", "commits", "index-race")); !os.IsNotExist(err) {
		t.Fatalf("ref created despite index race: %v", err)
	}
}

type countingRunner struct {
	base        SystemRunner
	commitTrees int
}

type editAfterTreeRunner struct {
	base SystemRunner
	path string
}

type indexEditRunner struct {
	base SystemRunner
	repo string
}

type failAfterRefRunner struct{ base SystemRunner }

func (r *failAfterRefRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	result, err := r.base.Run(ctx, dir, env, args...)
	if err == nil && len(args) > 0 && args[0] == "update-ref" {
		return result, errors.New("simulated lost response")
	}
	return result, err
}

func (r *indexEditRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	result, err := r.base.Run(ctx, dir, env, args...)
	if err == nil && len(args) > 0 && args[0] == "write-tree" {
		c := exec.Command("git", "add", "--", "a.txt")
		c.Dir = r.repo
		_ = c.Run()
	}
	return result, err
}

func (r *editAfterTreeRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	result, err := r.base.Run(ctx, dir, env, args...)
	if err == nil && len(args) > 0 && args[0] == "write-tree" {
		_ = os.WriteFile(r.path, []byte("changed concurrently\n"), 0600)
	}
	return result, err
}

func (r *countingRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	for _, arg := range args {
		if arg == "commit-tree" {
			r.commitTrees++
			break
		}
	}
	return r.base.Run(ctx, dir, env, args...)
}

type memoryIntentStore struct {
	intents    map[string]Intent
	commits    map[string]string
	reconciled map[string]string
}

func (s *memoryIntentStore) init() {
	if s.intents == nil {
		s.intents = map[string]Intent{}
	}
	if s.commits == nil {
		s.commits = map[string]string{}
	}
	if s.reconciled == nil {
		s.reconciled = map[string]string{}
	}
}
func (s *memoryIntentStore) PutCommitIntent(_ context.Context, i Intent) error {
	s.init()
	if old, ok := s.intents[i.ID]; ok && old != i {
		return os.ErrExist
	}
	s.intents[i.ID] = i
	return nil
}
func (s *memoryIntentStore) GetCommitIntent(_ context.Context, id string) (Intent, error) {
	s.init()
	i, ok := s.intents[id]
	if !ok {
		return Intent{}, os.ErrNotExist
	}
	return i, nil
}
func (s *memoryIntentStore) GetCommitRecord(ctx context.Context, id string) (Record, error) {
	i, err := s.GetCommitIntent(ctx, id)
	if err != nil {
		return Record{}, err
	}
	r := Record{Intent: i, State: "COMMIT_REQUESTED"}
	if sha := s.commits[id]; sha != "" {
		r.State, r.SHA = "CREATED", sha
	}
	if reason := s.reconciled[id]; reason != "" {
		r.State = "RECONCILE_REQUIRED"
	}
	return r, nil
}
func (s *memoryIntentStore) RecordCommit(_ context.Context, id, sha string) error {
	s.init()
	s.commits[id] = sha
	return nil
}
func (s *memoryIntentStore) RecordReconcile(_ context.Context, id, reason string) error {
	s.init()
	s.reconciled[id] = reason
	return nil
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--initial-branch=main")
	return dir
}
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func showRefs(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "show-ref")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil && len(out) != 0 {
		t.Fatalf("git show-ref: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}
func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func snapshotFile(t *testing.T, path, value string) SnapshotEntry {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return SnapshotEntry{Path: filepath.Base(path), Content: []byte(value), Mode: st.Mode()}
}
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustRun(t *testing.T, runner Runner, repo string, env map[string]string, args ...string) string {
	t.Helper()
	r, err := runner.Run(context.Background(), repo, env, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, r.Output)
	}
	return r.Output
}
func digest(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func emptyDigest() string    { return NoneEvidenceDigest }
