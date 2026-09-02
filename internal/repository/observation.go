package repository

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
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// CommandResult is the small read-only command result needed by repository
// observation. Keeping this port local prevents observation from acquiring
// Git mutation authority.
type CommandResult struct{ Output string }

type SystemRunner struct {
	Executable string
	MaxOutput  int
}

func (r SystemRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (CommandResult, error) {
	executable := r.Executable
	if executable == "" {
		executable = "git"
	}
	max := r.MaxOutput
	if max <= 0 {
		max = 1 << 20
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = dir
	command.Env = observationEnvironment(env)
	output := &boundedObservationOutput{max: max}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	if output.truncated && err == nil {
		err = io.ErrShortBuffer
	}
	return CommandResult{Output: string(output.bytes)}, err
}

type boundedObservationOutput struct {
	bytes     []byte
	max       int
	truncated bool
}

func (b *boundedObservationOutput) Write(value []byte) (int, error) {
	if len(b.bytes)+len(value) > b.max {
		remaining := b.max - len(b.bytes)
		if remaining > 0 {
			b.bytes = append(b.bytes, value[:remaining]...)
		}
		b.truncated = true
		return len(value), io.ErrShortBuffer
	}
	b.bytes = append(b.bytes, value...)
	return len(value), nil
}

func observationEnvironment(extra map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra)+3)
	for _, item := range os.Environ() {
		key := item
		if at := strings.IndexByte(item, '='); at >= 0 {
			key = item[:at]
		}
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "SSH_") || key == "CDPATH" {
			continue
		}
		env = append(env, item)
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+extra[key])
	}
	return env
}

type Runner interface {
	Run(context.Context, string, map[string]string, ...string) (CommandResult, error)
}

// IgnoreChecker is the optional read-only capability used to validate paths
// explicitly supplied by an adapter. Git status already omits ignored files,
// but an explicit path must not bypass that policy.
type IgnoreChecker interface {
	IsIgnored(context.Context, string, string) (bool, error)
}

// IsIgnored asks Git's configured ignore engine about one repository-relative
// path. A non-zero exit status of one means the path is not ignored; other
// failures are returned because ignore policy must fail closed.
func (r SystemRunner) IsIgnored(ctx context.Context, root, name string) (bool, error) {
	if err := validateRelativePath(name); err != nil {
		return false, err
	}
	executable := r.Executable
	if executable == "" {
		executable = "git"
	}
	command := exec.CommandContext(ctx, executable, "check-ignore", "--quiet", "--", name)
	command.Dir = root
	command.Env = observationEnvironment(nil)
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check ignore policy: %w", err)
}

// FileObservation is a bounded baseline observation. Content is retained only
// in memory for immediate ownership comparison; durable callers should store
// the resulting digests, not source bytes.
type FileObservation struct {
	Content []byte
	Mode    os.FileMode
	Present bool
}

type Baseline struct {
	Head         string
	IndexDigest  string
	StatusDigest string
	PathsDigest  string
	Paths        []string
	Files        map[string]FileObservation
}

type BaselineOptions struct {
	MaxFileSize int64
	Paths       []string
	// BeforeRead is a deterministic fault-injection hook for callers that
	// need to exercise replacement races. Production callers leave it nil.
	BeforeRead func(string)
}

const defaultBaselineFileSize int64 = 16 << 20

// EventPayload returns the redacted facts suitable for a session.started
// domain event. Raw paths, status text, and file contents never cross this
// boundary.
func (b Baseline) EventPayload() map[string]any {
	payload := map[string]any{
		"baseline_index":        b.IndexDigest,
		"status_digest":         b.StatusDigest,
		"baseline_paths_digest": b.PathsDigest,
	}
	if b.Head != "" {
		payload["baseline_head"] = b.Head
	}
	return payload
}

func (b Baseline) Clone() Baseline {
	n := b
	n.Paths = append([]string(nil), b.Paths...)
	n.Files = make(map[string]FileObservation, len(b.Files))
	for name, file := range b.Files {
		n.Files[name] = FileObservation{Content: append([]byte(nil), file.Content...), Mode: file.Mode, Present: file.Present}
	}
	return n
}

// CaptureBaseline records the repository state at a session/task boundary.
// Git is queried read-only and changed files are captured without following
// symlink components. An empty HEAD is valid for an unborn initial branch.
func CaptureBaseline(ctx context.Context, runner Runner, root string) (Baseline, error) {
	return CaptureBaselineWithOptions(ctx, runner, root, BaselineOptions{})
}

// CaptureCommittedFiles reads only explicitly requested regular files from an
// immutable Git tree. It is the safe restart boundary for clean sessions:
// durable state stores the tree identity, while source bytes are reconstructed
// transiently from Git and never written to the state database.
func CaptureCommittedFiles(ctx context.Context, runner Runner, root, head string, paths []string, maxFileSize int64) (map[string]FileObservation, error) {
	if runner == nil || root == "" {
		return nil, errors.New("committed-file runner and root are required")
	}
	if head != "" && !validObjectID(head) {
		return nil, errors.New("invalid committed-file HEAD")
	}
	if maxFileSize <= 0 {
		maxFileSize = defaultBaselineFileSize
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("committed-file root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, errors.New("committed-file root is not a directory")
	}
	files := make(map[string]FileObservation, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, name := range paths {
		if err := validateRelativePath(name); err != nil {
			return nil, fmt.Errorf("invalid committed-file path: %w", err)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if head == "" {
			files[name] = FileObservation{}
			continue
		}
		treeResult, runErr := runner.Run(ctx, abs, nil, "ls-tree", "-z", "--full-tree", head, "--", name)
		if runErr != nil {
			return nil, fmt.Errorf("read committed tree entry %q: %w", name, runErr)
		}
		mode, object, treePath, present, parseErr := parseCommittedTreeEntry(treeResult.Output)
		if parseErr != nil {
			return nil, fmt.Errorf("read committed tree entry %q: %w", name, parseErr)
		}
		if !present {
			files[name] = FileObservation{}
			continue
		}
		if treePath != name {
			return nil, fmt.Errorf("committed tree path mismatch: got %q, want %q", treePath, name)
		}
		if mode != 0100644 && mode != 0100755 {
			return nil, fmt.Errorf("committed tree entry %q is not a regular file", name)
		}
		blobResult, runErr := runner.Run(ctx, abs, nil, "cat-file", "blob", object)
		if runErr != nil {
			return nil, fmt.Errorf("read committed file %q: %w", name, runErr)
		}
		if int64(len(blobResult.Output)) > maxFileSize {
			return nil, fmt.Errorf("committed file %q exceeds capture limit", name)
		}
		fileMode := os.FileMode(0644)
		if mode == 0100755 {
			fileMode = 0755
		}
		files[name] = FileObservation{Content: append([]byte(nil), []byte(blobResult.Output)...), Mode: fileMode, Present: true}
	}
	return files, nil
}

func parseCommittedTreeEntry(raw string) (mode int64, object, name string, present bool, err error) {
	if raw == "" {
		return 0, "", "", false, nil
	}
	parts := strings.Split(raw, "\x00")
	if len(parts) != 2 || parts[0] == "" {
		return 0, "", "", false, errors.New("malformed Git tree entry")
	}
	fields := strings.SplitN(parts[0], "\t", 2)
	if len(fields) != 2 {
		return 0, "", "", false, errors.New("malformed Git tree metadata")
	}
	metadata := strings.Fields(fields[0])
	if len(metadata) != 3 || metadata[1] != "blob" || !validObjectID(metadata[2]) {
		return 0, "", "", false, errors.New("invalid Git tree metadata")
	}
	mode, err = strconv.ParseInt(metadata[0], 8, 32)
	if err != nil {
		return 0, "", "", false, errors.New("invalid Git tree mode")
	}
	return mode, metadata[2], fields[1], true, nil
}

// CaptureBaselineWithOptions records the same read-only repository facts as
// CaptureBaseline while bounding every regular file retained in memory.
func CaptureBaselineWithOptions(ctx context.Context, runner Runner, root string, options BaselineOptions) (Baseline, error) {
	if runner == nil || root == "" {
		return Baseline{}, errors.New("baseline runner and root are required")
	}
	maxFileSize := options.MaxFileSize
	if maxFileSize <= 0 {
		maxFileSize = defaultBaselineFileSize
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Baseline{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Baseline{}, fmt.Errorf("baseline root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Baseline{}, errors.New("baseline root is not a directory")
	}
	headResult, headErr := runner.Run(ctx, abs, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	head := strings.TrimSpace(headResult.Output)
	if headErr != nil && (head != "" || !isUnbornHeadError(headErr)) {
		return Baseline{}, fmt.Errorf("read HEAD: %w", headErr)
	}
	if head != "" && !validObjectID(head) {
		return Baseline{}, errors.New("invalid HEAD observation")
	}

	indexResult, err := runner.Run(ctx, abs, nil, "rev-parse", "--git-path", "index")
	if err != nil {
		return Baseline{}, fmt.Errorf("read index path: %w", err)
	}
	indexPath := strings.TrimSpace(indexResult.Output)
	if indexPath == "" {
		return Baseline{}, errors.New("empty index path")
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(abs, indexPath)
	}
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return Baseline{}, fmt.Errorf("read index: %w", err)
	}

	statusResult, err := runner.Run(ctx, abs, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return Baseline{}, fmt.Errorf("read status: %w", err)
	}
	paths, err := statusPaths(statusResult.Output)
	if err != nil {
		return Baseline{}, err
	}
	seenPaths := make(map[string]bool, len(paths)+len(options.Paths))
	for _, name := range paths {
		seenPaths[name] = true
	}
	for _, name := range options.Paths {
		if err := validateRelativePath(name); err != nil {
			return Baseline{}, fmt.Errorf("invalid baseline path: %w", err)
		}
		if checker, ok := runner.(IgnoreChecker); ok {
			ignored, ignoreErr := checker.IsIgnored(ctx, abs, name)
			if ignoreErr != nil {
				return Baseline{}, fmt.Errorf("check baseline path policy: %w", ignoreErr)
			}
			if ignored {
				return Baseline{}, fmt.Errorf("baseline path is ignored: %q", name)
			}
		}
		if !seenPaths[name] {
			seenPaths[name] = true
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	files := make(map[string]FileObservation, len(paths))
	for _, name := range paths {
		file, captureErr := captureBaselineFile(abs, name, maxFileSize, options.BeforeRead)
		if captureErr != nil {
			return Baseline{}, captureErr
		}
		files[name] = file
	}
	return Baseline{
		Head:         head,
		IndexDigest:  digestBytes(indexBytes),
		StatusDigest: digestBytes([]byte(statusResult.Output)),
		PathsDigest:  digestStrings(paths),
		Paths:        paths,
		Files:        files,
	}, nil
}

func isUnbornHeadError(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	// Git versions differ here: --verify --quiet reports an unborn HEAD as
	// either 1 or 128. Both are safe only when the command produced no HEAD.
	return exitErr.ExitCode() == 1 || exitErr.ExitCode() == 128
}

func statusPaths(raw string) ([]string, error) {
	parts := strings.Split(raw, "\x00")
	seen := map[string]bool{}
	paths := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		record := parts[i]
		if record == "" {
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return nil, errors.New("invalid git status record")
		}
		name := record[3:]
		if err := validateRelativePath(name); err != nil {
			return nil, fmt.Errorf("invalid status path: %w", err)
		}
		add := func(path string) error {
			if err := validateRelativePath(path); err != nil {
				return fmt.Errorf("invalid status path: %w", err)
			}
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
			return nil
		}
		if err := add(name); err != nil {
			return nil, err
		}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			if i+1 >= len(parts) || parts[i+1] == "" {
				return nil, errors.New("rename status record is missing source path")
			}
			i++
			if err := add(parts[i]); err != nil {
				return nil, err
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func captureBaselineFile(root, name string, maxFileSize int64, beforeRead func(string)) (FileObservation, error) {
	absolute, err := safeJoin(root, name)
	if err != nil {
		return FileObservation{}, err
	}
	if err := rejectSymlinkParents(root, absolute); err != nil {
		return FileObservation{}, err
	}
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return FileObservation{Present: false}, nil
	}
	if err != nil {
		return FileObservation{}, fmt.Errorf("observe %q: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if beforeRead != nil {
			beforeRead(name)
		}
		before, statErr := os.Lstat(absolute)
		if statErr != nil || !sameFileObservation(info, before) {
			return FileObservation{}, fmt.Errorf("observe %q changed during capture", name)
		}
		target, readErr := os.Readlink(absolute)
		if readErr != nil {
			return FileObservation{}, readErr
		}
		after, statErr := os.Lstat(absolute)
		if statErr != nil || !sameFileObservation(before, after) {
			return FileObservation{}, fmt.Errorf("observe %q changed during capture", name)
		}
		return FileObservation{Content: []byte(target), Mode: info.Mode(), Present: true}, nil
	}
	if !info.Mode().IsRegular() {
		return FileObservation{Mode: info.Mode(), Present: true}, nil
	}
	if info.Size() > maxFileSize {
		return FileObservation{}, fmt.Errorf("observe %q exceeds baseline capture limit", name)
	}
	if beforeRead != nil {
		beforeRead(name)
	}
	before, err := os.Lstat(absolute)
	if err != nil || !sameFileObservation(info, before) || !before.Mode().IsRegular() {
		return FileObservation{}, fmt.Errorf("observe %q changed during capture", name)
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return FileObservation{}, fmt.Errorf("observe %q: %w", name, err)
	}
	after, err := os.Lstat(absolute)
	if err != nil || !sameFileObservation(before, after) || !after.Mode().IsRegular() {
		return FileObservation{}, fmt.Errorf("observe %q changed during capture", name)
	}
	if int64(len(content)) > maxFileSize {
		return FileObservation{}, fmt.Errorf("observe %q exceeds baseline capture limit", name)
	}
	return FileObservation{Content: append([]byte(nil), content...), Mode: info.Mode(), Present: true}, nil
}

func sameFileObservation(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}

func safeJoin(root, name string) (string, error) {
	if err := validateRelativePath(name); err != nil {
		return "", err
	}
	absolute := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("status path escapes repository root")
	}
	return absolute, nil
}

func validateRelativePath(name string) error {
	if name == "" || strings.IndexByte(name, 0) >= 0 || filepath.IsAbs(filepath.FromSlash(name)) || strings.Contains(name, "\\") {
		return errors.New("path is not a safe repository-relative path")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("path contains a control character")
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("path contains an unsafe component")
		}
	}
	return nil
}

func rejectSymlinkParents(root, absolute string) error {
	rel, err := filepath.Rel(root, absolute)
	if err != nil {
		return err
	}
	current := root
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil
			}
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("status path contains a symlink component")
		}
	}
	return nil
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func digestBytes(value []byte) string {
	h := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(h[:])
}

func digestStrings(values []string) string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	return digestBytes([]byte(strings.Join(copyValues, "\x00")))
}

// DigestPaths returns the canonical identity for an explicit path set. It
// removes duplicate requests because repository observations are set-shaped.
func DigestPaths(values []string) string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return digestStrings(unique)
}

// EmptyStatusDigest identifies a repository with no tracked, staged, or
// untracked status records at a session boundary.
func EmptyStatusDigest() string { return digestBytes(nil) }
