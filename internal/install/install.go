// Package install owns the small, reversible configuration changes needed to
// connect an adapter hook. It never executes a hook and never reads an
// implicit user configuration location.
package install

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const OwnershipMarker = "autogit"

type Format string

const (
	FormatJSON  Format = "json"
	FormatLines Format = "lines"
	// FormatShell is a line-oriented format. It is named separately so callers
	// can document the client format without granting us shell execution.
	FormatShell Format = "shell"
)

type ConfigSpec struct {
	Adapter string
	Path    string
	Format  Format
}

type InstallPlan struct {
	Spec     ConfigSpec
	Path     string
	Original []byte
	Desired  []byte
	Exists   bool
	Mode     os.FileMode
	Changed  bool
}

var (
	ErrScope     = errors.New("configuration path is outside explicit roots")
	ErrFormat    = errors.New("unsupported or malformed configuration")
	ErrOwnership = errors.New("configuration entry is not owned by autogit")
	ErrStale     = errors.New("configuration changed after planning")
)

// Plan validates the target and computes a complete desired file without
// touching disk. roots must be explicit absolute directories supplied by the
// caller (normally a CLI flag or a platform discovery result).
func Plan(spec ConfigSpec, roots []string) (InstallPlan, error) {
	if spec.Adapter == "" || spec.Path == "" {
		return InstallPlan{}, fmt.Errorf("%w: adapter and path required", ErrScope)
	}
	if spec.Format == "" {
		spec.Format = FormatJSON
	}
	if spec.Format != FormatJSON && spec.Format != FormatLines && spec.Format != FormatShell {
		return InstallPlan{}, ErrFormat
	}
	path, err := checkedPath(spec.Path, roots)
	if err != nil {
		return InstallPlan{}, err
	}
	p := InstallPlan{Spec: spec, Path: path, Mode: 0600}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return InstallPlan{}, fmt.Errorf("%w: target is not a regular file", ErrScope)
		}
		p.Exists = true
		p.Mode = info.Mode()
		p.Original, err = os.ReadFile(path)
		if err != nil {
			return InstallPlan{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstallPlan{}, err
	}
	if p.Exists && len(p.Original) > 4<<20 {
		return InstallPlan{}, fmt.Errorf("%w: configuration exceeds limit", ErrFormat)
	}
	switch spec.Format {
	case FormatJSON:
		p.Desired, err = desiredJSON(spec.Adapter, p.Original, p.Exists)
	default:
		p.Desired, err = desiredLines(spec.Adapter, p.Original)
	}
	if err != nil {
		return InstallPlan{}, err
	}
	p.Changed = !bytes.Equal(p.Original, p.Desired) || (p.Exists && p.Mode.Perm() != 0600)
	return p, nil
}

// Apply performs a backup (when replacing existing bytes), then an atomic
// same-directory replacement with restrictive permissions. An unchanged plan
// is a no-op, which makes repeated installation idempotent.
func Apply(p InstallPlan) error {
	if p.Path == "" || p.Spec.Adapter == "" {
		return ErrScope
	}
	if !p.Changed {
		return nil
	}
	dir := filepath.Dir(p.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(p.Path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrScope
		}
		current, readErr := os.ReadFile(p.Path)
		if readErr != nil {
			return readErr
		}
		if !p.Exists || !bytes.Equal(current, p.Original) {
			return ErrStale
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if p.Exists {
		return ErrStale
	}
	if p.Exists {
		backup := p.Path + ".autogit-backup-" + backupID()
		if err := writeExclusive(backup, p.Original, 0600); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".autogit-install-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(p.Desired)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, p.Path); err != nil {
		return err
	}
	_ = os.Chmod(p.Path, 0600)
	return syncDir(dir)
}

// Uninstall removes only an entry carrying AutoGit's ownership marker. A
// foreign marker or unrelated line is preserved and reported as ownership.
func Uninstall(spec ConfigSpec, roots []string) error {
	p, err := Plan(spec, roots)
	if err != nil {
		return err
	}
	if !p.Exists {
		return nil
	}
	var desired []byte
	switch spec.Format {
	case FormatJSON:
		desired, err = removeJSON(spec.Adapter, p.Original)
	default:
		desired, err = removeLine(spec.Adapter, p.Original)
	}
	if err != nil {
		return err
	}
	if bytes.Equal(desired, p.Original) {
		return nil
	}
	p.Desired, p.Changed = desired, true
	return Apply(p)
}

// Discover returns only existing regular config files whose paths are inside
// the explicit roots. It intentionally does not consult HOME or environment
// defaults.
func Discover(specs []ConfigSpec, roots []string) ([]ConfigSpec, error) {
	var out []ConfigSpec
	for _, spec := range specs {
		path, err := checkedPath(spec.Path, roots)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			spec.Path = path
			out = append(out, spec)
		}
	}
	return out, nil
}

func checkedPath(path string, roots []string) (string, error) {
	if len(roots) == 0 || !filepath.IsAbs(path) {
		return "", ErrScope
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			continue
		}
		base := filepath.Clean(root)
		if info, err := os.Lstat(base); err == nil && info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		rel, err := filepath.Rel(base, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Lstat(clean); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", ErrScope
		}
		// Every existing parent is checked for symlink escapes. This catches
		// link/config even when the final config does not yet exist.
		for cur := filepath.Dir(clean); cur != base && strings.HasPrefix(cur, base+string(filepath.Separator)); cur = filepath.Dir(cur) {
			if info, err := os.Lstat(cur); err == nil && info.Mode()&os.ModeSymlink != 0 {
				return "", ErrScope
			}
		}
		return clean, nil
	}
	return "", ErrScope
}

func desiredJSON(adapter string, original []byte, exists bool) ([]byte, error) {
	obj := map[string]any{}
	if exists {
		if err := decodeJSON(original, &obj); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFormat, err)
		}
	}
	if marker, ok := obj[OwnershipMarker]; ok {
		if !ownedMarker(marker, adapter) {
			return nil, ErrOwnership
		}
		return json.Marshal(obj)
	}
	obj[OwnershipMarker] = map[string]any{"managed_by": "autogit", "adapter": adapter, "version": 1, "hook": "autogit hook --adapter " + adapter}
	return json.Marshal(obj)
}

func removeJSON(adapter string, original []byte) ([]byte, error) {
	obj := map[string]any{}
	if err := decodeJSON(original, &obj); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	marker, ok := obj[OwnershipMarker]
	if !ok {
		return original, nil
	}
	if !ownedMarker(marker, adapter) {
		return nil, ErrOwnership
	}
	delete(obj, OwnershipMarker)
	return json.Marshal(obj)
}

func ownedMarker(value any, adapter string) bool {
	m, ok := value.(map[string]any)
	if !ok {
		return false
	}
	managed, _ := m["managed_by"].(string)
	owner, _ := m["adapter"].(string)
	return managed == "autogit" && owner == adapter
}

func desiredLines(adapter string, original []byte) ([]byte, error) {
	marker := lineMarker(adapter)
	for _, line := range strings.Split(strings.ReplaceAll(string(original), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "# autogit:") && strings.Contains(line, "managed_by=autogit") {
			if line != marker {
				return nil, ErrOwnership
			}
			return original, nil
		}
	}
	base := string(original)
	if base != "" && !strings.HasSuffix(base, "\n") {
		base += "\n"
	}
	return []byte(base + marker + "\n"), nil
}

func removeLine(adapter string, original []byte) ([]byte, error) {
	marker := lineMarker(adapter)
	lines := strings.SplitAfter(string(original), "\n")
	found := false
	var b strings.Builder
	for _, line := range lines {
		if strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") == marker {
			found = true
			continue
		}
		b.WriteString(line)
	}
	if !found {
		return original, nil
	}
	return []byte(b.String()), nil
}

func lineMarker(adapter string) string { return "# autogit:managed_by=autogit;adapter=" + adapter }
func writeExclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
func backupID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return fmt.Sprintf("%x", b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return nil
	}
	return nil
}

func decodeJSON(data []byte, out any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := decodeValue(dec, out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing data")
		}
		return err
	}
	return nil
}

// decodeValue uses a token walk solely to detect duplicate keys before the
// standard decoder stores an object in a map.
func decodeValue(dec *json.Decoder, out any) error {
	var raw any
	if err := parseTokenValue(dec, &raw); err != nil {
		return err
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func parseTokenValue(dec *json.Decoder, out *any) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); ok {
		switch d {
		case '{':
			m := map[string]any{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok {
					return errors.New("object key")
				}
				if _, exists := m[key]; exists {
					return fmt.Errorf("duplicate key %q", key)
				}
				var v any
				if err := parseTokenValue(dec, &v); err != nil {
					return err
				}
				m[key] = v
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
			*out = m
			return nil
		case '[':
			var a []any
			for dec.More() {
				var v any
				if err := parseTokenValue(dec, &v); err != nil {
					return err
				}
				a = append(a, v)
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
			*out = a
			return nil
		}
	}
	*out = t
	return nil
}
