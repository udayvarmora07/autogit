package repository

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	durableBaselineVersion    = 1
	maxDurableBaselineBytes   = 4 << 20
	maxDurableBaselineFiles   = 100000
	durableBaselinePathPrefix = "hmac-sha256:"
)

var durableDigestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// DurableFileEvidence is a source-free baseline fingerprint. PathID is an
// HMAC of the repository-relative path, so the manifest can be retained for
// restart recovery without exposing raw filenames in SQLite state.
type DurableFileEvidence struct {
	PathID        string `json:"path_id"`
	ContentDigest string `json:"content_digest,omitempty"`
	Mode          uint32 `json:"mode,omitempty"`
	Present       bool   `json:"present"`
	Symlink       bool   `json:"symlink,omitempty"`
}

// DurableBaseline is the bounded, source-free evidence needed to compare a
// later status observation with a session-start baseline.
type DurableBaseline struct {
	Version     int                   `json:"version"`
	PathsDigest string                `json:"paths_digest"`
	Files       []DurableFileEvidence `json:"files"`
}

// EncodeDurableBaseline creates deterministic restart evidence. It contains
// no path text and no source bytes; only key-bound path identifiers and
// content/mode fingerprints cross the process boundary.
func EncodeDurableBaseline(b Baseline, key []byte) (string, error) {
	if len(key) == 0 {
		return "", errors.New("durable baseline identity key is required")
	}
	paths := append([]string(nil), b.Paths...)
	seen := make(map[string]bool, len(paths)+len(b.Files))
	for _, name := range paths {
		if err := validateRelativePath(name); err != nil {
			return "", fmt.Errorf("durable baseline path: %w", err)
		}
		seen[name] = true
	}
	for name := range b.Files {
		if err := validateRelativePath(name); err != nil {
			return "", fmt.Errorf("durable baseline path: %w", err)
		}
		if !seen[name] {
			paths = append(paths, name)
			seen[name] = true
		}
	}
	if len(paths) > maxDurableBaselineFiles {
		return "", errors.New("durable baseline contains too many paths")
	}
	sort.Strings(paths)
	pathsDigest := DigestPaths(paths)
	if b.PathsDigest != "" && b.PathsDigest != pathsDigest {
		return "", errors.New("durable baseline path digest does not match paths")
	}
	evidence := DurableBaseline{Version: durableBaselineVersion, PathsDigest: pathsDigest, Files: make([]DurableFileEvidence, 0, len(paths))}
	for _, name := range paths {
		file := b.Files[name]
		entry := DurableFileEvidence{PathID: durablePathID(key, name), Mode: durableMode(file.Mode), Present: file.Present, Symlink: file.Symlink}
		if file.Present && !file.Symlink && file.Mode.IsRegular() {
			entry.ContentDigest = digestBytes(file.Content)
		}
		evidence.Files = append(evidence.Files, entry)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	if len(raw) > maxDurableBaselineBytes {
		return "", errors.New("durable baseline evidence exceeds size limit")
	}
	return string(raw), nil
}

// DecodeDurableBaseline validates source-free restart evidence before it is
// used for ownership decisions.
func DecodeDurableBaseline(raw string) (DurableBaseline, error) {
	if len(raw) == 0 || len(raw) > maxDurableBaselineBytes {
		return DurableBaseline{}, errors.New("invalid durable baseline size")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	var evidence DurableBaseline
	if err := decoder.Decode(&evidence); err != nil {
		return DurableBaseline{}, errors.New("invalid durable baseline evidence")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return DurableBaseline{}, errors.New("invalid durable baseline trailing input")
	}
	if evidence.Version != durableBaselineVersion || !durableDigestRE.MatchString(evidence.PathsDigest) || len(evidence.Files) > maxDurableBaselineFiles {
		return DurableBaseline{}, errors.New("invalid durable baseline identity")
	}
	seen := make(map[string]bool, len(evidence.Files))
	for _, file := range evidence.Files {
		if !strings.HasPrefix(file.PathID, durableBaselinePathPrefix) || len(file.PathID) != len(durableBaselinePathPrefix)+64 || seen[file.PathID] {
			return DurableBaseline{}, errors.New("invalid durable baseline path identity")
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(file.PathID, durableBaselinePathPrefix)); err != nil {
			return DurableBaseline{}, errors.New("invalid durable baseline path identity")
		}
		seen[file.PathID] = true
		if file.ContentDigest != "" && !durableDigestRE.MatchString(file.ContentDigest) {
			return DurableBaseline{}, errors.New("invalid durable baseline content digest")
		}
		if file.Symlink && file.ContentDigest != "" {
			return DurableBaseline{}, errors.New("symlink baseline contains content evidence")
		}
		if !file.Present && file.ContentDigest != "" {
			return DurableBaseline{}, errors.New("absent baseline contains content evidence")
		}
	}
	return evidence, nil
}

func durablePathID(key []byte, name string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("autogit/path\x00"))
	_, _ = h.Write([]byte(name))
	return durableBaselinePathPrefix + hex.EncodeToString(h.Sum(nil))
}

// DurablePathID returns the key-bound identity used in a durable baseline
// manifest. Callers use it to match fresh status paths without recovering
// private baseline filenames from persisted state.
func DurablePathID(key []byte, name string) (string, error) {
	if len(key) == 0 {
		return "", errors.New("durable baseline identity key is required")
	}
	if err := validateRelativePath(name); err != nil {
		return "", err
	}
	return durablePathID(key, name), nil
}

func durableMode(mode os.FileMode) uint32 {
	if mode.Perm()&0111 != 0 {
		return 0755
	}
	return 0644
}
