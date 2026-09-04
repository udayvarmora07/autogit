package gittransaction

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// SnapshotAtCommit reads the complete file tree of one immutable commit using
// read-only Git object operations. It never reads the worktree and returns
// symlink entries as typed metadata so a caller can fail closed during a
// publication scan.
func SnapshotAtCommit(ctx context.Context, runner Runner, root, commit string, maxTotalBytes int64) ([]SnapshotEntry, error) {
	if runner == nil || root == "" || !oidRE.MatchString(commit) {
		return nil, errors.New("commit tree snapshot request is invalid")
	}
	if maxTotalBytes <= 0 {
		maxTotalBytes = 64 << 20
	}
	canonical, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(canonical) != canonical {
		return nil, errors.New("commit tree root is not canonical")
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return nil, errors.New("commit tree root is not a directory")
	}
	listing, err := runTreeSnapshot(ctx, runner, canonical, 64<<20, "ls-tree", "-r", "-z", "--full-tree", commit, "--")
	if err != nil {
		return nil, fmt.Errorf("read commit tree: %w", err)
	}
	if listing.Output == "" {
		return nil, errors.New("commit tree is empty")
	}
	entries := make([]SnapshotEntry, 0)
	seen := make(map[string]bool)
	var total int64
	for _, raw := range strings.Split(listing.Output, "\x00") {
		if raw == "" {
			continue
		}
		entry, mode, object, err := parseTreeSnapshotRecord(raw)
		if err != nil {
			return nil, err
		}
		if seen[entry] {
			return nil, fmt.Errorf("commit tree contains duplicate path %q", entry)
		}
		seen[entry] = true
		if mode == 0160000 {
			return nil, fmt.Errorf("commit tree contains unsupported submodule %q", entry)
		}
		content, err := runTreeSnapshot(ctx, runner, canonical, maxTotalBytes-total, "cat-file", "blob", object)
		if err != nil {
			return nil, fmt.Errorf("read commit blob %q: %w", entry, err)
		}
		if int64(len(content.Output)) > maxTotalBytes-total {
			return nil, fmt.Errorf("commit tree exceeds content limit at %q", entry)
		}
		total += int64(len(content.Output))
		fileMode := os.FileMode(0644)
		switch mode {
		case 0100755:
			fileMode = 0755
		case 0120000:
			fileMode = os.FileMode(0120000)
		case 0100644:
		default:
			return nil, fmt.Errorf("commit tree entry %q has unsupported mode", entry)
		}
		entries = append(entries, SnapshotEntry{Path: entry, Content: []byte(content.Output), Mode: fileMode})
	}
	if len(entries) == 0 {
		return nil, errors.New("commit tree is empty")
	}
	return entries, nil
}

type treeSnapshotBoundedRunner interface {
	RunBounded(context.Context, string, map[string]string, int, ...string) (Result, error)
}

func runTreeSnapshot(ctx context.Context, runner Runner, root string, max int64, args ...string) (Result, error) {
	if max <= 0 || max > int64(^uint(0)>>1) {
		return Result{}, errors.New("invalid commit tree output limit")
	}
	if bounded, ok := runner.(treeSnapshotBoundedRunner); ok {
		return bounded.RunBounded(ctx, root, nil, int(max), args...)
	}
	result, err := runner.Run(ctx, root, nil, args...)
	if err != nil {
		return Result{}, err
	}
	if int64(len(result.Output)) > max {
		return Result{}, io.ErrShortBuffer
	}
	return result, nil
}

func parseTreeSnapshotRecord(raw string) (string, int64, string, error) {
	parts := strings.SplitN(raw, "\t", 2)
	if len(parts) != 2 {
		return "", 0, "", errors.New("malformed commit tree record")
	}
	fields := strings.Fields(parts[0])
	if len(fields) != 3 {
		return "", 0, "", errors.New("malformed commit tree metadata")
	}
	mode, err := strconv.ParseInt(fields[0], 8, 32)
	if err != nil || !oidRE.MatchString(fields[2]) {
		return "", 0, "", errors.New("invalid commit tree metadata")
	}
	if fields[1] != "blob" && fields[1] != "commit" {
		return "", 0, "", errors.New("invalid commit tree object type")
	}
	name := parts[1]
	if err := safeTreePath(name); err != nil {
		return "", 0, "", err
	}
	return name, mode, fields[2], nil
}

func safeTreePath(name string) error {
	if name == "" || strings.ContainsAny(name, "\\\x00\r\n") || filepath.IsAbs(filepath.FromSlash(name)) {
		return errors.New("commit tree contains unsafe path")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("commit tree contains control path")
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("commit tree contains unsafe path")
		}
	}
	return nil
}
