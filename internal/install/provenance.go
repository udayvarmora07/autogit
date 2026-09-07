package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ProvenanceSchemaVersion = "autogit.config-provenance/1"

type Provenance struct {
	SchemaVersion string   `json:"schema_version"`
	Adapter       string   `json:"adapter"`
	ConfigDigest  string   `json:"config_digest"`
	Installed     bool     `json:"installed"`
	RecordedAt    string   `json:"recorded_at"`
	Backups       []string `json:"backups,omitempty"`
}

type MigrationPreview struct {
	Path          string `json:"path_redacted"`
	Adapter       string `json:"adapter"`
	Changed       bool   `json:"changed"`
	CurrentDigest string `json:"current_digest,omitempty"`
	DesiredDigest string `json:"desired_digest"`
}

func ProvenancePath(configPath string) string { return configPath + ".autogit-provenance.json" }

func ApplyWithProvenance(plan InstallPlan) error {
	if err := Apply(plan); err != nil {
		return err
	}
	return record(plan.Path, plan.Spec.Adapter, true)
}

func RemoveWithProvenance(plan InstallPlan) error {
	if err := Apply(plan); err != nil {
		return err
	}
	return record(plan.Path, plan.Spec.Adapter, false)
}

func Preview(plan InstallPlan) MigrationPreview {
	return MigrationPreview{Path: "redacted", Adapter: plan.Spec.Adapter, Changed: plan.Changed, CurrentDigest: digest(plan.Original), DesiredDigest: digest(plan.Desired)}
}

func ReadProvenance(configPath string) (Provenance, error) {
	provenancePath := ProvenancePath(configPath)
	info, err := os.Lstat(provenancePath)
	if err != nil {
		return Provenance{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Provenance{}, ErrScope
	}
	raw, err := os.ReadFile(provenancePath) // #nosec G304 -- caller has already validated an explicit config path.
	if err != nil {
		return Provenance{}, err
	}
	var p Provenance
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return Provenance{}, fmt.Errorf("invalid configuration provenance: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF || p.SchemaVersion != ProvenanceSchemaVersion || p.Adapter == "" || p.ConfigDigest == "" {
		return Provenance{}, errors.New("invalid configuration provenance")
	}
	return p, nil
}

func Rollback(configPath string, adapter string) error {
	p, err := ReadProvenance(configPath)
	if err != nil {
		return err
	}
	if p.Adapter != adapter || len(p.Backups) == 0 {
		return ErrOwnership
	}
	backups := append([]string(nil), p.Backups...)
	sort.Slice(backups, func(i, j int) bool {
		left, _ := os.Stat(backups[i])
		right, _ := os.Stat(backups[j])
		return left.ModTime().After(right.ModTime())
	})
	backup := backups[0]
	if filepath.Dir(filepath.Clean(backup)) != filepath.Dir(filepath.Clean(configPath)) || !strings.HasPrefix(filepath.Base(backup), filepath.Base(configPath)+".autogit-backup-") {
		return ErrScope
	}
	info, err := os.Lstat(backup)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrScope
	}
	raw, err := os.ReadFile(backup) // #nosec G304 -- provenance selected a same-config backup path.
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".autogit-rollback-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, configPath); err != nil {
		return err
	}
	return record(configPath, adapter, true)
}

func record(configPath, adapter string, installed bool) error {
	raw, err := os.ReadFile(configPath) // #nosec G304 -- configPath comes from an explicit, validated install plan.
	if err != nil {
		return err
	}
	backups, _ := filepath.Glob(configPath + ".autogit-backup-*")
	for i := range backups {
		backups[i] = filepath.Clean(backups[i])
	}
	p := Provenance{SchemaVersion: ProvenanceSchemaVersion, Adapter: adapter, ConfigDigest: digest(raw), Installed: installed, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Backups: backups}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".autogit-provenance-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, ProvenancePath(configPath))
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
