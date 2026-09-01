// Package gittransaction creates a verified local commit object without
// touching the user's worktree, index, or current branch.
package gittransaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"autogit/internal/commit"
)

const maxOutput = 1 << 20

// Intent is the complete immutable identity of a local commit operation.
// TreeOID and ParentSHA are discovered by this package, never trusted from a
// client claim. Ref is always an AutoGit-owned ref.
type Intent struct {
	ID, RepoDir, Ref, ParentSHA, TreeOID, Message  string
	CandidateDigest, MessageDigest, SnapshotDigest string
	PolicyDigest, VerifierDigest, GuardDigest      string
}

type Record struct {
	Intent     Intent
	SHA, State string
}

// IntentPort is the durable boundary. PutCommitIntent must commit its record
// before returning. Implementations must reject an existing ID with a
// different immutable identity.
type IntentPort interface {
	PutCommitIntent(context.Context, Intent) error
	GetCommitIntent(context.Context, string) (Intent, error)
	RecordCommit(context.Context, string, string) error
	RecordReconcile(context.Context, string, string) error
}

type Runner interface {
	Run(context.Context, string, map[string]string, ...string) (Result, error)
}

type Result struct {
	Output    string
	Err       error
	Truncated bool
}

// SystemRunner invokes git directly with an argument array. It never invokes
// a shell and bounds combined stdout/stderr to avoid unbounded diagnostics.
type SystemRunner struct {
	Executable string
	MaxOutput  int
}

func (r SystemRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	return r.RunBounded(ctx, dir, env, r.MaxOutput, args...)
}

// RunBounded lets higher-level read-only workflows apply a per-operation
// output budget while retaining the same controlled environment and direct
// argv execution as Run.
func (r SystemRunner) RunBounded(ctx context.Context, dir string, env map[string]string, max int, args ...string) (Result, error) {
	if r.Executable == "" {
		r.Executable = "git"
	}
	if max <= 0 {
		max = r.MaxOutput
	}
	if max <= 0 {
		max = maxOutput
	}
	c := exec.CommandContext(ctx, r.Executable, args...)
	c.Dir = dir
	c.Env = controlledEnv(env)
	b := &boundedBuffer{max: max}
	c.Stdout, c.Stderr = b, b
	err := c.Run()
	if b.truncated && err == nil {
		err = io.ErrShortBuffer
	}
	return Result{Output: string(b.buf), Err: err, Truncated: b.truncated}, err
}

func controlledEnv(extra map[string]string) []string {
	base := make([]string, 0, 8+len(extra))
	for _, item := range os.Environ() {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		// Ambient Git/SSH settings can redirect object databases, indexes,
		// config, hooks, or credentials, so only ordinary process settings are
		// inherited by this bounded local transaction.
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "SSH_") || key == "CDPATH" {
			continue
		}
		if key == "PATH" || key == "HOME" || key == "TMPDIR" || strings.HasPrefix(key, "LANG") || strings.HasPrefix(key, "LC_") {
			base = append(base, item)
		}
	}
	base = append(base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := extra[k]
		base = append(base, k+"="+v)
	}
	return base
}

type boundedBuffer struct {
	buf       []byte
	max       int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(b.buf)+len(p) > b.max {
		n := b.max - len(b.buf)
		if n > 0 {
			b.buf = append(b.buf, p[:n]...)
		}
		b.truncated = true
		return len(p), io.ErrShortBuffer
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

type Request struct {
	ID      string
	RepoDir string
	// Snapshot is an immutable, caller-owned copy of the paths to include. The
	// transaction never reads mutable worktree bytes to construct a candidate.
	Snapshot []SnapshotEntry
	// Paths is retained only for source compatibility with the early staging
	// API; an omitted immutable snapshot is rejected by validateRequest.
	Paths   []string
	Message string
	// Ref, when supplied, must equal refs/autogit/commits/<ID>.
	Ref                                       string
	PolicyDigest, VerifierDigest, GuardDigest string
}

type SnapshotEntry struct {
	Path    string
	Content []byte
	Mode    os.FileMode
	Delete  bool
}

type Commit struct {
	SHA, TreeOID, ParentSHA, Ref, CandidateDigest, MessageDigest string
	State                                                        string
}

type Transaction struct {
	git     Runner
	intents IntentPort
}

func New(git Runner, intents IntentPort) *Transaction {
	return &Transaction{git: git, intents: intents}
}

var idRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var oidRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var digestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// NoneEvidenceDigest is the explicit digest callers may use when a policy,
// verifier, or guard set is intentionally empty. Empty strings are rejected.
var NoneEvidenceDigest = messageDigest("none")

func (t *Transaction) Create(ctx context.Context, req Request) (Commit, error) {
	if t == nil || t.git == nil || t.intents == nil {
		return Commit{}, errors.New("git transaction dependencies missing")
	}
	if err := validateRequest(req); err != nil {
		return Commit{}, err
	}
	root, err := repositoryRoot(ctx, t.git, req.RepoDir)
	if err != nil {
		return Commit{}, err
	}
	entries, _, err := validateSnapshot(root, req.Snapshot)
	if err != nil {
		return Commit{}, err
	}
	// An existing durable intent is authoritative. Reconcile it from the
	// AutoGit ref before doing any candidate or commit work; this is the
	// idempotency boundary that prevents duplicate commit objects/effects.
	if existing, getErr := t.intents.GetCommitIntent(ctx, req.ID); getErr == nil {
		if existing.RepoDir != root {
			return Commit{}, errors.New("commit intent repository conflict")
		}
		if existing.SnapshotDigest != snapshotDigest(entries) || existing.MessageDigest != messageDigest(canonicalMessage(req.Message)) || existing.PolicyDigest != req.PolicyDigest || existing.VerifierDigest != req.VerifierDigest || existing.GuardDigest != req.GuardDigest {
			return Commit{}, errors.New("commit intent identity conflict")
		}
		return t.Recover(ctx, req.ID)
	} else if !errors.Is(getErr, os.ErrNotExist) {
		return Commit{}, getErr
	}
	parent, indexPath, indexBefore, err := observe(ctx, t.git, root)
	if err != nil {
		return Commit{}, err
	}
	tmp, err := os.CreateTemp("", "autogit-index-")
	if err != nil {
		return Commit{}, fmt.Errorf("temporary index: %w", err)
	}
	index := tmp.Name()
	_ = tmp.Close()
	if err := os.Remove(index); err != nil && !os.IsNotExist(err) {
		return Commit{}, err
	}
	defer os.Remove(index)
	env := map[string]string{"GIT_INDEX_FILE": index}
	// Candidate preparation writes only unreachable blob/tree objects and a
	// temporary index. It cannot alter a ref, worktree, or user index. The
	// durable intent is persisted below immediately before commit-tree and the
	// AutoGit ref mutation; recovery never retries this ref effect blindly.
	if parent == "" {
		if _, err = t.git.Run(ctx, root, env, "read-tree", "--empty"); err != nil {
			return Commit{}, fmt.Errorf("initialize candidate index: %w", err)
		}
	} else if _, err = t.git.Run(ctx, root, env, "read-tree", "--reset", parent); err != nil {
		return Commit{}, fmt.Errorf("initialize candidate index: %w", err)
	}
	for _, entry := range entries {
		if entry.Delete {
			if _, err = t.git.Run(ctx, root, env, "update-index", "--remove", "--", entry.Path); err != nil {
				return Commit{}, fmt.Errorf("stage deletion: %w", err)
			}
			continue
		}
		blobFile, fileErr := os.CreateTemp("", "autogit-blob-")
		if fileErr != nil {
			return Commit{}, fileErr
		}
		blobPath := blobFile.Name()
		if chmodErr := blobFile.Chmod(0600); chmodErr == nil {
			_, fileErr = blobFile.Write(entry.Content)
		} else {
			fileErr = chmodErr
		}
		if closeErr := blobFile.Close(); fileErr == nil {
			fileErr = closeErr
		}
		if fileErr != nil {
			_ = os.Remove(blobPath)
			return Commit{}, fmt.Errorf("snapshot blob: %w", fileErr)
		}
		blobResult, blobErr := t.git.Run(ctx, root, env, "hash-object", "-w", "--", blobPath)
		_ = os.Remove(blobPath)
		if blobErr != nil {
			return Commit{}, fmt.Errorf("write snapshot blob: %w", blobErr)
		}
		blob := strings.TrimSpace(blobResult.Output)
		if !oidRE.MatchString(blob) {
			return Commit{}, errors.New("git returned invalid snapshot blob")
		}
		cacheInfo := fmt.Sprintf("%s,%s,%s", gitMode(entry.Mode), blob, entry.Path)
		if _, err = t.git.Run(ctx, root, env, "update-index", "--add", "--cacheinfo", cacheInfo); err != nil {
			return Commit{}, fmt.Errorf("stage snapshot: %w", err)
		}
	}
	treeResult, err := t.git.Run(ctx, root, env, "write-tree")
	if err != nil {
		return Commit{}, fmt.Errorf("write candidate tree: %w", err)
	}
	tree := strings.TrimSpace(treeResult.Output)
	if !oidRE.MatchString(tree) {
		return Commit{}, errors.New("git returned invalid candidate tree")
	}
	if err := ensureUnchanged(ctx, t.git, root, parent, indexPath, indexBefore); err != nil {
		return Commit{}, err
	}

	message := canonicalMessage(req.Message)
	intent := Intent{ID: req.ID, RepoDir: root, Ref: refFor(req.ID, req.Ref), ParentSHA: parent, TreeOID: tree,
		Message: message, CandidateDigest: treeDigest(tree), MessageDigest: messageDigest(message),
		SnapshotDigest: snapshotDigest(entries), PolicyDigest: req.PolicyDigest, VerifierDigest: req.VerifierDigest, GuardDigest: req.GuardDigest}
	if err := t.intents.PutCommitIntent(ctx, intent); err != nil {
		return Commit{}, err
	}
	// A durable intent now exists before commit-tree or ref mutation.
	messageFile, err := os.CreateTemp("", "autogit-message-")
	if err != nil {
		_ = t.intents.RecordReconcile(ctx, req.ID, "message file: "+err.Error())
		return Commit{}, err
	}
	messagePath := messageFile.Name()
	defer os.Remove(messagePath)
	if err := messageFile.Chmod(0600); err == nil {
		_, err = io.WriteString(messageFile, message)
	} else {
		err = fmt.Errorf("message permissions: %w", err)
	}
	if closeErr := messageFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = t.intents.RecordReconcile(ctx, req.ID, "message file: "+err.Error())
		return Commit{}, err
	}
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, "-F", messagePath)
	commitResult, err := t.git.Run(ctx, root, env, args...)
	if err != nil {
		_ = t.intents.RecordReconcile(ctx, req.ID, "commit-tree outcome unknown")
		return Commit{}, fmt.Errorf("create commit object: %w", err)
	}
	sha := strings.TrimSpace(commitResult.Output)
	if !oidRE.MatchString(sha) {
		_ = t.intents.RecordReconcile(ctx, req.ID, "invalid commit object identity")
		return Commit{}, errors.New("git returned invalid commit sha")
	}
	if err := ensureUnchanged(ctx, t.git, root, parent, indexPath, indexBefore); err != nil {
		_ = t.intents.RecordReconcile(ctx, req.ID, "repository changed before ref update")
		return Commit{}, err
	}
	oldRef, err := inspectRef(ctx, t.git, root, intent.Ref)
	if err != nil {
		return t.reconcile(ctx, req.ID, "inspect AutoGit ref: "+err.Error())
	}
	if oldRef != "" {
		if oldRef != sha {
			return t.reconcile(ctx, req.ID, "AutoGit ref already names a different commit")
		}
		if err := t.intents.RecordCommit(ctx, req.ID, sha); err != nil {
			return Commit{}, err
		}
		return commitResultOf(intent, sha), nil
	}
	if _, err := t.git.Run(ctx, root, nil, "update-ref", "--no-deref", intent.Ref, sha, ""); err != nil {
		return t.reconcile(ctx, req.ID, "ref update outcome unknown")
	}
	if err := verifyCommit(ctx, t.git, root, intent, sha); err != nil {
		return t.reconcile(ctx, req.ID, "commit postcondition failed")
	}
	if err := t.intents.RecordCommit(ctx, req.ID, sha); err != nil {
		return Commit{}, err
	}
	return commitResultOf(intent, sha), nil
}

// Recover proves a prior intent's side effect from the AutoGit ref and commit
// object. It never invokes commit-tree and therefore cannot duplicate a commit.
func (t *Transaction) Recover(ctx context.Context, id string) (Commit, error) {
	if t == nil || t.git == nil || t.intents == nil {
		return Commit{}, errors.New("git transaction dependencies missing")
	}
	if !idRE.MatchString(id) {
		return Commit{}, errors.New("invalid commit intent ID")
	}
	i, err := t.intents.GetCommitIntent(ctx, id)
	if err != nil {
		return Commit{}, err
	}
	if err := validateIntent(i); err != nil {
		return Commit{}, err
	}
	sha, err := inspectRef(ctx, t.git, i.RepoDir, i.Ref)
	if err != nil {
		return t.reconcile(ctx, id, "inspect AutoGit ref: "+err.Error())
	}
	if sha == "" {
		return t.reconcile(ctx, id, "AutoGit ref is absent")
	}
	if err := verifyCommit(ctx, t.git, i.RepoDir, i, sha); err != nil {
		return t.reconcile(ctx, id, "commit evidence mismatch")
	}
	if err := t.intents.RecordCommit(ctx, id, sha); err != nil {
		return Commit{}, err
	}
	return commitResultOf(i, sha), nil
}

func (t *Transaction) reconcile(ctx context.Context, id, reason string) (Commit, error) {
	_ = t.intents.RecordReconcile(ctx, id, reason)
	return Commit{}, fmt.Errorf("%s: %s", "reconcile required", reason)
}

func validateRequest(r Request) error {
	if !idRE.MatchString(r.ID) {
		return errors.New("invalid commit intent ID")
	}
	if r.RepoDir == "" {
		return errors.New("repository directory is required")
	}
	if len(r.Snapshot) == 0 {
		return errors.New("immutable owned snapshot is required")
	}
	if err := commit.Validate(canonicalMessage(r.Message)); err != nil {
		return err
	}
	if !digestRE.MatchString(r.PolicyDigest) || !digestRE.MatchString(r.VerifierDigest) || !digestRE.MatchString(r.GuardDigest) {
		return errors.New("invalid evidence digest")
	}
	if r.Ref != "" && r.Ref != refFor(r.ID, "") {
		return errors.New("invalid AutoGit ref")
	}
	return nil
}
func validateIntent(i Intent) error {
	if !idRE.MatchString(i.ID) || i.RepoDir == "" || !filepath.IsAbs(i.RepoDir) || strings.IndexByte(i.RepoDir, 0) >= 0 || filepath.Clean(i.RepoDir) != i.RepoDir || i.Ref != refFor(i.ID, "") || !oidOrEmpty(i.ParentSHA) || !oidRE.MatchString(i.TreeOID) || !digestRE.MatchString(i.CandidateDigest) || !digestRE.MatchString(i.MessageDigest) || !digestRE.MatchString(i.SnapshotDigest) || !digestRE.MatchString(i.PolicyDigest) || !digestRE.MatchString(i.VerifierDigest) || !digestRE.MatchString(i.GuardDigest) {
		return errors.New("invalid commit intent")
	}
	if err := commit.Validate(i.Message); err != nil {
		return errors.New("invalid commit intent")
	}
	return nil
}
func oidOrEmpty(s string) bool { return s == "" || oidRE.MatchString(s) }
func refFor(id, supplied string) string {
	if supplied != "" {
		return supplied
	}
	return "refs/autogit/commits/" + id
}
func canonicalMessage(s string) string { return strings.TrimSpace(s) + "\n" }
func treeDigest(tree string) string {
	h := sha256.Sum256([]byte("git-tree\x00" + tree))
	return "sha256:" + hex.EncodeToString(h[:])
}
func messageDigest(message string) string {
	h := sha256.Sum256([]byte(message))
	return "sha256:" + hex.EncodeToString(h[:])
}
func snapshotDigest(entries []SnapshotEntry) string {
	canonical := append([]SnapshotEntry(nil), entries...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].Path < canonical[j].Path })
	h := sha256.New()
	for _, entry := range canonical {
		fmt.Fprintf(h, "%s\x00%s\x00%t\x00", entry.Path, gitMode(entry.Mode), entry.Delete)
		if !entry.Delete {
			_, _ = h.Write(entry.Content)
		}
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
func commitResultOf(i Intent, sha string) Commit {
	return Commit{SHA: sha, TreeOID: i.TreeOID, ParentSHA: i.ParentSHA, Ref: i.Ref, CandidateDigest: i.CandidateDigest, MessageDigest: i.MessageDigest, State: "CREATED"}
}

func repositoryRoot(ctx context.Context, g Runner, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", errors.New("invalid repository directory")
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", errors.New("repository directory is not a directory")
	}
	r, err := g.Run(ctx, abs, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("not a Git repository")
	}
	root, err := filepath.Abs(strings.TrimSpace(r.Output))
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil || root != abs {
		return "", errors.New("repository directory must be the Git root")
	}
	return root, nil
}

func validateSnapshot(root string, requested []SnapshotEntry) ([]SnapshotEntry, []string, error) {
	seen := map[string]bool{}
	entries := make([]SnapshotEntry, 0, len(requested))
	paths := make([]string, 0, len(requested))
	for _, input := range requested {
		p := input.Path
		if p == "" || strings.ContainsAny(p, "\x00\r\n\\") || filepath.IsAbs(p) {
			return nil, nil, errors.New("unsafe owned path")
		}
		clean := filepath.Clean(filepath.FromSlash(p))
		if clean == "." || clean != filepath.FromSlash(p) {
			return nil, nil, errors.New("unsafe owned path")
		}
		for _, part := range strings.Split(clean, string(filepath.Separator)) {
			if part == ".." {
				return nil, nil, errors.New("unsafe owned path")
			}
		}
		abs := filepath.Join(root, clean)
		if !within(root, abs) {
			return nil, nil, errors.New("owned path escapes repository")
		}
		if err := rejectSymlinkComponents(root, clean); err != nil {
			return nil, nil, err
		}
		if !input.Delete {
			if input.Mode&os.ModeSymlink != 0 || input.Mode&os.ModeType != 0 {
				return nil, nil, errors.New("snapshot entry is not a regular file")
			}
			input.Content = append([]byte(nil), input.Content...)
			if input.Mode == 0 {
				input.Mode = 0644
			}
		}
		if seen[clean] {
			return nil, nil, errors.New("duplicate owned path")
		}
		seen[clean] = true
		paths = append(paths, clean)
		input.Path = clean
		entries = append(entries, input)
	}
	if len(paths) == 0 {
		return nil, nil, errors.New("no owned paths")
	}
	return entries, paths, nil
}

func gitMode(mode os.FileMode) string {
	if mode.Perm()&0111 != 0 {
		return "100755"
	}
	return "100644"
}
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func rejectSymlinkComponents(root, path string) error {
	cur := root
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink owned path is unsafe")
		}
		if st.IsDir() && cur != filepath.Join(root, path) {
			continue
		}
	}
	return nil
}

func observe(ctx context.Context, g Runner, root string) (parent, index string, indexBytes []byte, err error) {
	res, e := g.Run(ctx, root, nil, "rev-parse", "--verify", "HEAD^{commit}")
	if e == nil {
		parent = strings.TrimSpace(res.Output)
		if !oidRE.MatchString(parent) {
			return "", "", nil, errors.New("invalid HEAD")
		}
	}
	idx, e := g.Run(ctx, root, nil, "rev-parse", "--git-path", "index")
	if e != nil {
		return "", "", nil, e
	}
	index = strings.TrimSpace(idx.Output)
	if !filepath.IsAbs(index) {
		index = filepath.Join(root, index)
	}
	indexBytes, e = os.ReadFile(index)
	if e != nil && !os.IsNotExist(e) {
		return "", "", nil, e
	}
	return parent, index, indexBytes, nil
}
func ensureUnchanged(ctx context.Context, g Runner, root, parent, index string, before []byte) error {
	res, err := g.Run(ctx, root, nil, "rev-parse", "--verify", "HEAD^{commit}")
	current := ""
	if err == nil {
		current = strings.TrimSpace(res.Output)
	}
	if current != parent {
		return errors.New("repository HEAD changed during transaction")
	}
	after, err := os.ReadFile(index)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !sameBytes(before, after) {
		return errors.New("user index changed during transaction")
	}
	return nil
}
func sameBytes(a, b []byte) bool { return string(a) == string(b) }

func inspectRef(ctx context.Context, g Runner, root, ref string) (string, error) {
	if !strings.HasPrefix(ref, "refs/autogit/commits/") {
		return "", errors.New("invalid AutoGit ref")
	}
	r, err := g.Run(ctx, root, nil, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", nil
	}
	sha := strings.TrimSpace(r.Output)
	if !oidRE.MatchString(sha) {
		return "", errors.New("invalid AutoGit ref target")
	}
	return sha, nil
}

func verifyCommit(ctx context.Context, g Runner, root string, i Intent, sha string) error {
	if !oidRE.MatchString(sha) {
		return errors.New("invalid commit SHA")
	}
	r, err := g.Run(ctx, root, nil, "cat-file", "commit", sha)
	if err != nil {
		return err
	}
	text := r.Output
	lineEnd := strings.IndexByte(text, '\n')
	if lineEnd < 0 || !strings.HasPrefix(text, "tree ") || strings.TrimSpace(text[5:lineEnd]) != i.TreeOID {
		return errors.New("tree evidence mismatch")
	}
	rest := text[lineEnd+1:]
	if i.ParentSHA != "" {
		if !strings.HasPrefix(rest, "parent "+i.ParentSHA+"\n") {
			return errors.New("parent evidence mismatch")
		}
		rest = rest[len("parent "+i.ParentSHA)+1:]
	}
	blank := strings.Index(rest, "\n\n")
	if blank < 0 {
		return errors.New("commit message missing")
	}
	body := rest[blank+2:]
	if messageDigest(body) != i.MessageDigest {
		return errors.New("message evidence mismatch")
	}
	return nil
}
