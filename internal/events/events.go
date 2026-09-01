package events

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"autogit"
	"github.com/santhosh-tekuri/jsonschema/v6"
	_ "modernc.org/sqlite"
)

type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func schema(code, message string) error { return &Error{Code: code, Message: message} }

type Event struct {
	SchemaVersion string         `json:"schema_version"`
	EventClass    string         `json:"event_class"`
	EventID       string         `json:"event_id"`
	EventType     string         `json:"event_type"`
	OccurredAt    string         `json:"occurred_at"`
	Producer      map[string]any `json:"producer"`
	Scope         map[string]any `json:"scope"`
	Ordering      map[string]any `json:"ordering"`
	Idempotency   map[string]any `json:"idempotency"`
	Capabilities  map[string]any `json:"capabilities,omitempty"`
	Project       map[string]any `json:"project,omitempty"`
	Payload       map[string]any `json:"payload"`
	Extensions    map[string]any `json:"extensions,omitempty"`
	Digest        string         `json:"-"`
}

var idRE = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
var digestRE = regexp.MustCompile(`^(sha256|hmac-sha256):[a-f0-9]{64}$`)
var auditDigestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var auditReasonRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var gitSHARe = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var eventTypes = map[string]bool{"session.started": true, "session.idle": true, "session.ended": true, "session.crashed": true, "session.recovered": true, "prompt.submitted": true, "prompt.requested": true, "prompt.queued": true, "prompt.presented": true, "prompt.answered": true, "prompt.expired": true, "prompt.cancelled": true, "task.started": true, "task.updated": true, "task.completion_candidate": true, "task.completed": true, "task.failed": true, "task.cancelled": true, "tool.started": true, "tool.completed": true, "files.changed": true, "model.stopped": true, "repository.discovered": true, "policy.consent_requested": true, "policy.set": true, "change.detected": true, "change.staged": true, "change.invalidated": true, "verification.requested": true, "verification.started": true, "verification.passed": true, "verification.failed": true, "verification.invalidated": true, "commit.requested": true, "commit.created": true, "commit.failed": true, "commit.reconciled": true, "push.requested": true, "push.succeeded": true, "push.failed": true, "push.skipped": true}
var ingressTypes = map[string]bool{"session.started": true, "session.idle": true, "session.ended": true, "prompt.submitted": true, "task.started": true, "task.updated": true, "task.completed": true, "task.failed": true, "tool.started": true, "tool.completed": true, "files.changed": true, "model.stopped": true}
var domainTypes = map[string]bool{"repository.discovered": true, "policy.consent_requested": true, "policy.set": true, "session.crashed": true, "session.recovered": true, "prompt.requested": true, "prompt.queued": true, "prompt.presented": true, "prompt.answered": true, "prompt.expired": true, "prompt.cancelled": true, "task.completion_candidate": true, "task.completed": true, "task.failed": true, "task.cancelled": true, "change.detected": true, "change.staged": true, "change.invalidated": true, "verification.requested": true, "verification.started": true, "verification.passed": true, "verification.failed": true, "verification.invalidated": true, "commit.requested": true, "commit.created": true, "commit.failed": true, "commit.reconciled": true, "push.requested": true, "push.succeeded": true, "push.failed": true, "push.skipped": true}
var adapterNames = map[string]bool{"codex": true, "claude-code": true, "cursor": true, "gemini-cli": true, "opencode": true, "commandcode": true}

const canonicalSchemaURL = "https://autogit.dev/schemas/event-v1.schema.json"

var canonicalSchema struct {
	once sync.Once
	sch  *jsonschema.Schema
	err  error
}

func embeddedSchema() (*jsonschema.Schema, error) {
	canonicalSchema.once.Do(func() {
		var raw []byte
		raw, canonicalSchema.err = autogit.CanonicalEventSchema.ReadFile("schemas/event-v1.schema.json")
		if canonicalSchema.err != nil {
			return
		}
		var doc any
		doc, canonicalSchema.err = jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if canonicalSchema.err != nil {
			return
		}
		compiler := jsonschema.NewCompiler()
		if canonicalSchema.err = compiler.AddResource(canonicalSchemaURL, doc); canonicalSchema.err != nil {
			return
		}
		canonicalSchema.sch, canonicalSchema.err = compiler.Compile(canonicalSchemaURL)
	})
	return canonicalSchema.sch, canonicalSchema.err
}

func validateEmbeddedSchema(instance map[string]any) error {
	sch, err := embeddedSchema()
	if err != nil {
		return schema("E_SCHEMA", "canonical event schema unavailable")
	}
	if err := sch.Validate(instance); err != nil {
		return schema("E_SCHEMA", "event does not satisfy canonical schema")
	}
	return nil
}

// Decode accepts one bounded JSON object, rejects duplicate keys and validates
// the canonical v1 envelope before any durable or external operation.
func Decode(input []byte, max int64) (Event, error) {
	if max <= 0 || int64(len(input)) > max {
		return Event{}, schema("E_SCHEMA", "event exceeds input limit")
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return Event{}, schema("E_SCHEMA", "malformed JSON")
	}
	if t, err := dec.Token(); err != io.EOF {
		_ = t
		return Event{}, schema("E_SCHEMA", "trailing JSON")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return Event{}, schema("E_SCHEMA", "event must be an object")
	}
	// Preserve the stable version and event-type error classes before schema
	// validation turns these into generic validation failures.
	if raw, exists := m["schema_version"]; exists {
		if version, ok := raw.(string); ok && strings.HasPrefix(version, "autogit.event/") && version != "autogit.event/1" {
			return Event{}, schema("E_VERSION", "unsupported event major")
		}
	}
	if raw, exists := m["event_type"]; exists {
		if eventType, ok := raw.(string); ok && !eventTypes[eventType] {
			return Event{}, schema("E_EVENT_TYPE", "unknown event type")
		}
	}
	if err := validateEmbeddedSchema(m); err != nil {
		return Event{}, err
	}
	canonical, _ := json.Marshal(m)
	var e Event
	b, _ := json.Marshal(m)
	if json.Unmarshal(b, &e) != nil {
		return Event{}, schema("E_SCHEMA", "invalid envelope")
	}
	if e.SchemaVersion != "autogit.event/1" {
		if strings.HasPrefix(e.SchemaVersion, "autogit.event/") {
			return Event{}, schema("E_VERSION", "unsupported event major")
		}
		return Event{}, schema("E_SCHEMA", "invalid schema version")
	}
	if err := validate(e, m); err != nil {
		return Event{}, err
	}
	h := sha256.Sum256(canonical)
	e.Digest = "sha256:" + hex.EncodeToString(h[:])
	return e, nil
}

func decodeValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '{':
			m := map[string]any{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return nil, err
				}
				key, ok := k.(string)
				if !ok {
					return nil, fmt.Errorf("key")
				}
				if _, exists := m[key]; exists {
					return nil, fmt.Errorf("duplicate key")
				}
				val, err := decodeValue(d)
				if err != nil {
					return nil, err
				}
				m[key] = val
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				val, err := decodeValue(d)
				if err != nil {
					return nil, err
				}
				a = append(a, val)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return a, nil
		}
	}
	return t, nil
}

func validate(e Event, raw map[string]any) error {
	allowed := map[string]bool{"schema_version": true, "event_class": true, "event_id": true, "event_type": true, "occurred_at": true, "producer": true, "scope": true, "ordering": true, "idempotency": true, "capabilities": true, "project": true, "payload": true, "extensions": true}
	for k := range raw {
		if !allowed[k] {
			return schema("E_SCHEMA", "unknown envelope field")
		}
	}
	if e.EventClass != "ingress" && e.EventClass != "domain" {
		return schema("E_SCHEMA", "invalid event class")
	}
	if !idRE.MatchString(e.EventID) {
		return schema("E_SCHEMA", "invalid event id")
	}
	if e.EventType == "" {
		return schema("E_SCHEMA", "event_type is required")
	}
	if !eventTypes[e.EventType] {
		return schema("E_EVENT_TYPE", "unknown event type")
	}
	if _, err := time.Parse(time.RFC3339, e.OccurredAt); err != nil || !strings.HasSuffix(e.OccurredAt, "Z") {
		return schema("E_SCHEMA", "occurred_at must be UTC RFC3339")
	}
	if e.Producer == nil || e.Scope == nil || e.Ordering == nil || e.Idempotency == nil || e.Payload == nil {
		return schema("E_SCHEMA", "missing required object")
	}
	if e.Scope["repo_id"] == nil || e.Ordering["stream_id"] == nil || e.Idempotency["key"] == nil {
		return schema("E_SCHEMA", "missing required scope/idempotency field")
	}
	if s := stringValue(e.Scope["repo_id"]); !digestRE.MatchString(s) {
		return schema("E_SCHEMA", "invalid repository digest")
	}
	if !opaque(stringValue(e.Ordering["stream_id"])) || !opaque(stringValue(e.Idempotency["key"])) {
		return schema("E_SCHEMA", "invalid ordering or idempotency value")
	}
	if v, ok := e.Ordering["producer_seq"]; ok && !validNonNegativeInteger(v) {
		return schema("E_SCHEMA", "invalid producer sequence")
	}
	if v, ok := e.Idempotency["attempt"]; ok && !validPositiveInteger(v) {
		return schema("E_SCHEMA", "invalid attempt")
	}
	if v, ok := e.Ordering["causation_id"]; ok && !idRE.MatchString(stringValue(v)) {
		return schema("E_SCHEMA", "invalid causation id")
	}
	if v, ok := e.Ordering["correlation_id"]; ok && !opaque(stringValue(v)) {
		return schema("E_SCHEMA", "invalid correlation id")
	}
	for _, k := range []string{"worktree_id"} {
		if v, ok := e.Scope[k]; ok && !digestRE.MatchString(stringValue(v)) {
			return schema("E_SCHEMA", "invalid scope digest")
		}
	}
	for _, k := range []string{"session_id", "task_id", "change_id"} {
		if v, ok := e.Scope[k]; ok && !opaque(stringValue(v)) {
			return schema("E_SCHEMA", "invalid scope id")
		}
	}
	for _, k := range []string{"producer", "scope", "ordering", "idempotency", "capabilities", "project", "payload", "extensions"} {
		if m, ok := raw[k].(map[string]any); !ok && raw[k] != nil {
			return schema("E_SCHEMA", "invalid "+k+" object")
		} else if m != nil {
			if err := validateObjectKeys(k, m); err != nil {
				return err
			}
		}
	}
	if e.EventClass == "ingress" {
		if e.Producer["kind"] != "adapter" || !adapterNames[stringValue(e.Producer["adapter"])] || stringValue(e.Producer["version"]) == "" || stringValue(e.Producer["installation_id"]) == "" || stringValue(e.Producer["instance_id"]) == "" || !ingressTypes[e.EventType] {
			return schema("E_SCHEMA", "invalid ingress producer or event type")
		}
	}
	if e.EventClass == "domain" {
		k := stringValue(e.Producer["kind"])
		if k != "core" && k != "reconciler" || stringValue(e.Producer["instance_id"]) == "" || e.Project != nil || !domainTypes[e.EventType] {
			return schema("E_SCHEMA", "invalid domain producer or event type")
		}
		if e.Ordering["correlation_id"] == nil {
			return schema("E_SCHEMA", "domain event requires correlation")
		}
	}
	if c, ok := e.Capabilities["queue_state"]; ok {
		switch stringValue(c) {
		case "native", "none", "unknown":
		default:
			return schema("E_SCHEMA", "invalid queue capability")
		}
	}
	if c, ok := e.Capabilities["task_boundaries"]; ok {
		switch stringValue(c) {
		case "native", "none", "synthetic":
		default:
			return schema("E_SCHEMA", "invalid task capability")
		}
	}
	if c, ok := e.Capabilities["changed_paths"]; ok {
		switch stringValue(c) {
		case "reported", "none", "derived":
		default:
			return schema("E_SCHEMA", "invalid path capability")
		}
	}
	if c, ok := e.Capabilities["monotonic_sequence"]; ok {
		if _, valid := c.(bool); !valid {
			return schema("E_SCHEMA", "invalid monotonic capability")
		}
	}
	if e.Project != nil {
		for _, k := range []string{"candidate_root", "client_cwd"} {
			if v, ok := e.Project[k]; ok {
				p, valid := v.(string)
				if !valid || p == "" || len(p) > 4096 {
					return schema("E_SCHEMA", "invalid project path")
				}
			}
		}
	}
	if strings.HasPrefix(e.EventType, "session.") && e.Scope["session_id"] == nil {
		return schema("E_SCHEMA", "session event requires session")
	}
	if (strings.HasPrefix(e.EventType, "task.") || strings.HasPrefix(e.EventType, "prompt.") || strings.HasPrefix(e.EventType, "tool.")) && (e.Scope["session_id"] == nil || e.Scope["task_id"] == nil) {
		return schema("E_SCHEMA", "task event requires session and task")
	}
	if strings.HasPrefix(e.EventType, "change.") || strings.HasPrefix(e.EventType, "verification.") || strings.HasPrefix(e.EventType, "commit.") || strings.HasPrefix(e.EventType, "push.") {
		for _, k := range []string{"worktree_id", "session_id", "task_id", "change_id"} {
			if e.Scope[k] == nil {
				return schema("E_SCHEMA", "change event missing scope")
			}
		}
	}
	if e.EventType == "files.changed" {
		changes, ok := e.Payload["changes"].([]any)
		if !ok || len(changes) == 0 || len(changes) > 10000 {
			return schema("E_SCHEMA", "files.changed requires changes")
		}
		for _, item := range changes {
			cm, ok := item.(map[string]any)
			if !ok {
				return schema("E_SCHEMA", "invalid change object")
			}
			for k := range cm {
				if k != "path" && k != "operation" && k != "previous_path" {
					return schema("E_SCHEMA", "unknown change field")
				}
			}
			path := stringValue(cm["path"])
			if path == "" || len(path) > 4096 || strings.HasPrefix(path, "/") || strings.Contains(path, "\x00") || strings.Contains(path, "\\") {
				return schema("E_SCOPE", "unsafe change path")
			}
			parts := strings.Split(path, "/")
			for _, part := range parts {
				if part == ".." || part == "" {
					return schema("E_SCOPE", "unsafe change path")
				}
			}
			op := stringValue(cm["operation"])
			switch op {
			case "added", "modified", "deleted", "renamed", "mode-changed", "unknown":
			default:
				return schema("E_SCHEMA", "invalid change operation")
			}
			if prev, ok := cm["previous_path"]; ok {
				p, valid := prev.(string)
				if !valid || p == "" || len(p) > 4096 || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
					return schema("E_SCOPE", "unsafe previous path")
				}
			}
		}
	}
	if strings.HasPrefix(e.EventType, "prompt.") {
		if !opaque(stringValue(e.Payload["prompt_id"])) || e.Payload["prompt_kind"] == nil {
			return schema("E_SCHEMA", "prompt event requires prompt facts")
		}
	}
	if strings.HasPrefix(e.EventType, "verification.") {
		if !opaque(stringValue(e.Payload["verification_id"])) || !digestRE.MatchString(stringValue(e.Payload["candidate_digest"])) {
			return schema("E_SCHEMA", "verification event requires digest evidence")
		}
	}
	if strings.HasPrefix(e.EventType, "commit.") {
		if !opaque(stringValue(e.Payload["commit_job_id"])) || !digestRE.MatchString(stringValue(e.Payload["candidate_digest"])) {
			return schema("E_SCHEMA", "commit event requires intent evidence")
		}
	}
	if strings.HasPrefix(e.EventType, "push.") {
		if !opaque(stringValue(e.Payload["push_job_id"])) || !gitSHARe.MatchString(stringValue(e.Payload["commit_sha"])) {
			return schema("E_SCHEMA", "push event requires exact commit")
		}
	}
	if v, ok := e.Payload["outcome"]; ok {
		switch stringValue(v) {
		case "success", "failure", "cancelled", "unknown":
		default:
			return schema("E_SCHEMA", "invalid outcome")
		}
	}
	if v, ok := e.Payload["prompt_kind"]; ok {
		switch stringValue(v) {
		case "consent", "verification", "repair", "notification", "unknown":
		default:
			return schema("E_SCHEMA", "invalid prompt kind")
		}
	}
	if v, ok := e.Payload["answer"]; ok {
		switch stringValue(v) {
		case "yes", "no", "approved", "rejected", "cancelled", "unknown":
		default:
			return schema("E_SCHEMA", "invalid prompt answer")
		}
	}
	for _, key := range []string{"base_head_digest", "tree_digest", "index_digest", "candidate_digest", "message_digest", "remote_digest"} {
		if v, ok := e.Payload[key]; ok && !digestRE.MatchString(stringValue(v)) {
			return schema("E_SCHEMA", "invalid evidence digest")
		}
	}
	for _, key := range []string{"candidate_revision", "exit_code", "duration_ms"} {
		if v, ok := e.Payload[key]; ok && !validNonNegativeInteger(v) {
			return schema("E_SCHEMA", "invalid numeric payload")
		}
	}
	if v, ok := e.Payload["blocking"]; ok {
		if _, valid := v.(bool); !valid {
			return schema("E_SCHEMA", "invalid blocking payload")
		}
	}
	if v, ok := e.Payload["commit_sha"]; ok && !gitSHARe.MatchString(stringValue(v)) {
		return schema("E_SCHEMA", "invalid commit sha")
	}
	return nil
}
func stringValue(v any) string           { s, _ := v.(string); return s }
func validNonNegativeInteger(v any) bool { n, ok := numberValue(v); return ok && n >= 0 }
func validPositiveInteger(v any) bool    { n, ok := numberValue(v); return ok && n >= 1 }
func numberValue(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case float64:
		i := int64(n)
		return i, i == int64(n)
	default:
		return 0, false
	}
}
func opaque(s string) bool {
	return len(s) > 0 && len(s) <= 256 && regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`).MatchString(s)
}
func validateObjectKeys(name string, m map[string]any) error {
	sets := map[string]map[string]bool{
		"producer":    {"kind": true, "adapter": true, "version": true, "installation_id": true, "instance_id": true, "client_version": true},
		"scope":       {"repo_id": true, "worktree_id": true, "session_id": true, "task_id": true, "change_id": true},
		"ordering":    {"stream_id": true, "producer_seq": true, "causation_id": true, "correlation_id": true},
		"idempotency": {"key": true, "attempt": true}, "capabilities": {"queue_state": true, "task_boundaries": true, "changed_paths": true, "monotonic_sequence": true},
		"project": {"candidate_root": true, "client_cwd": true}, "extensions": {},
		"payload": {"status": true, "state": true, "outcome": true, "reason": true, "error_code": true, "prompt_id": true, "prompt_kind": true, "blocking": true, "answer": true, "changes": true, "candidate_revision": true, "base_head_digest": true, "tree_digest": true, "index_digest": true, "candidate_digest": true, "verification_id": true, "verifier_set": true, "verifier_version": true, "exit_code": true, "duration_ms": true, "commit_job_id": true, "commit_sha": true, "message_digest": true, "push_job_id": true, "remote_digest": true, "ref": true, "extensions": true},
	}
	allowed, exists := sets[name]
	if !exists {
		return nil
	}
	if name == "extensions" {
		return nil
	}
	for k := range m {
		if !allowed[k] {
			return schema("E_SCHEMA", "unknown "+name+" field")
		}
	}
	return nil
}

type Disposition string

const (
	Accepted  Disposition = "accepted"
	Duplicate Disposition = "duplicate"
	Pending   Disposition = "pending"
	Rejected  Disposition = "rejected"
	Conflict  Disposition = "conflict"
)

type Receipt struct {
	Disposition   Disposition
	Revision      int64
	StateRevision int64
	Reason        string
}

// AuditLog is the deliberately small public diagnostics record. Raw metadata
// remains private to the event store and is never returned to CLI callers.
type AuditLog struct {
	Timestamp   string `json:"timestamp"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
	EventDigest string `json:"event_digest"`
	Revision    int64  `json:"revision"`
}

// ProjectionReceipt is a lifecycle receipt update produced by a projector.
// It allows a causal replay to promote buffered receipts in the same SQLite
// transaction as the projection update without making events depend on the
// lifecycle package.
type ProjectionReceipt struct {
	EventID     string
	Digest      string
	Disposition Disposition
}

// ProjectionResult is returned by a pure, typed projector. Data is opaque to
// this package and must be a bounded representation owned by the caller.
type ProjectionResult struct {
	Data        []byte
	Disposition Disposition
	Reason      string
	Revision    int64
	Receipts    []ProjectionReceipt
}

type Projector func(current []byte, event Event) (ProjectionResult, error)

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	_ = os.Chmod(filepath.Dir(path), 0700)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS event_receipts (event_id TEXT PRIMARY KEY, idempotency_key TEXT NOT NULL UNIQUE, payload_digest TEXT NOT NULL, disposition TEXT NOT NULL, revision INTEGER NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS pending_events (event_id TEXT PRIMARY KEY, causation_id TEXT NOT NULL, payload BLOB NOT NULL); CREATE TABLE IF NOT EXISTS audit_events (revision INTEGER PRIMARY KEY AUTOINCREMENT, repository_id TEXT NOT NULL DEFAULT '', disposition TEXT NOT NULL DEFAULT '', reason_code TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS lifecycle_projections (repository_id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state BLOB NOT NULL, updated_at TEXT NOT NULL);`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureAuditRepositoryColumn(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureAuditColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0600)
	return &Store{db: db}, nil
}

func ensureAuditRepositoryColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(audit_events)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	present := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "repository_id" {
			present = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if present {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE audit_events ADD COLUMN repository_id TEXT NOT NULL DEFAULT ''`)
	return err
}

func ensureAuditColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(audit_events)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range []string{"disposition", "reason_code"} {
		if present[column] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE audit_events ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Close() error { return s.db.Close() }

// AcceptAndProject validates receipt identity and invokes projector while the
// receipt, causal buffer, and bounded projection are held in one transaction.
// A projector error rolls back every write, including an otherwise accepted
// receipt.
func (s *Store) AcceptAndProject(ctx context.Context, e Event, projector Projector) (Receipt, error) {
	if projector == nil {
		return Receipt{}, errors.New("projection callback is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	key := stringValue(e.Idempotency["key"])
	var oldDigest, oldID string
	var oldRev int64
	idErr := tx.QueryRowContext(ctx, `SELECT event_id,payload_digest,revision FROM event_receipts WHERE event_id=?`, e.EventID).Scan(&oldID, &oldDigest, &oldRev)
	var keyDigest, keyID string
	var keyRev int64
	keyErr := tx.QueryRowContext(ctx, `SELECT event_id,payload_digest,revision FROM event_receipts WHERE idempotency_key=?`, key).Scan(&keyID, &keyDigest, &keyRev)
	if idErr == nil || keyErr == nil {
		if idErr == nil && keyErr == nil && oldID == e.EventID && keyID == e.EventID && oldDigest == e.Digest && keyDigest == e.Digest {
			var stateRevision int64
			var stateData []byte
			if err := tx.QueryRowContext(ctx, `SELECT state,revision FROM lifecycle_projections WHERE repository_id=?`, stringValue(e.Scope["repo_id"])).Scan(&stateData, &stateRevision); errors.Is(err, sql.ErrNoRows) {
				return Receipt{}, schema("E_PROJECTION_MIGRATION", "receipt exists without lifecycle projection; rebuild is required")
			} else if err != nil {
				return Receipt{}, err
			} else if len(stateData) == 0 {
				return Receipt{}, schema("E_PROJECTION_MIGRATION", "lifecycle projection is empty; rebuild is required")
			}
			return Receipt{Disposition: Duplicate, Revision: oldRev, StateRevision: stateRevision}, tx.Commit()
		}
		return Receipt{Disposition: Conflict, Reason: "E_IDEMPOTENCY_CONFLICT"}, schema("E_IDEMPOTENCY_CONFLICT", "event identity was reused with different content")
	}
	if !errors.Is(idErr, sql.ErrNoRows) || !errors.Is(keyErr, sql.ErrNoRows) {
		if idErr != nil && !errors.Is(idErr, sql.ErrNoRows) {
			return Receipt{}, idErr
		}
		return Receipt{}, keyErr
	}
	var current []byte
	var projectionRevision int64
	repoID := stringValue(e.Scope["repo_id"])
	err = tx.QueryRowContext(ctx, `SELECT state,revision FROM lifecycle_projections WHERE repository_id=?`, repoID).Scan(&current, &projectionRevision)
	hasProjection := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		current = nil
		projectionRevision = 0
	} else if err != nil {
		return Receipt{}, err
	}
	if len(current) > 1<<20 {
		return Receipt{}, errors.New("lifecycle projection exceeds size limit")
	}
	projected, err := projector(current, e)
	if err != nil {
		return Receipt{}, err
	}
	if len(projected.Data) == 0 {
		return Receipt{}, errors.New("projection data is empty")
	}
	if projected.Disposition == "" {
		projected.Disposition = Accepted
	}
	if projected.Disposition != Accepted && projected.Disposition != Pending && projected.Disposition != Rejected {
		return Receipt{}, errors.New("invalid projection disposition")
	}
	if projected.Revision < projectionRevision {
		return Receipt{}, errors.New("projection revision regressed")
	}
	// Pending buffers can change without advancing the reducer revision; store
	// those bytes as well. The single SQLite connection serializes the CAS.
	if !hasProjection || projected.Revision >= projectionRevision {
		if _, err = tx.ExecContext(ctx, `INSERT INTO lifecycle_projections(repository_id,revision,state,updated_at) VALUES(?,?,?,?) ON CONFLICT(repository_id) DO UPDATE SET revision=excluded.revision,state=excluded.state,updated_at=excluded.updated_at`, repoID, projected.Revision, projected.Data, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return Receipt{}, err
		}
	}
	rev := int64(0)
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM event_receipts`).Scan(&rev); err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_receipts(event_id,idempotency_key,payload_digest,disposition,revision,created_at) VALUES(?,?,?,?,?,?)`, e.EventID, key, e.Digest, projected.Disposition, rev, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Receipt{}, err
	}
	if projected.Disposition == Pending {
		causation := stringValue(e.Ordering["causation_id"])
		b, _ := boundedPendingJSON(e)
		if _, err = tx.ExecContext(ctx, `INSERT INTO pending_events(event_id,causation_id,payload) VALUES(?,?,?)`, e.EventID, causation, b); err != nil {
			return Receipt{}, err
		}
	}
	for _, update := range projected.Receipts {
		if update.EventID == "" || update.EventID == e.EventID {
			continue
		}
		if update.Digest == "" {
			continue
		}
		if update.Disposition != Accepted && update.Disposition != Pending && update.Disposition != Rejected {
			return Receipt{}, errors.New("invalid projection receipt disposition")
		}
		changed, err := tx.ExecContext(ctx, `UPDATE event_receipts SET disposition=? WHERE event_id=? AND payload_digest=? AND disposition=?`, update.Disposition, update.EventID, update.Digest, Pending)
		if err != nil {
			return Receipt{}, err
		}
		if affected, err := changed.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return Receipt{}, err
			}
			return Receipt{}, errors.New("projection receipt update diverged from pending buffer")
		}
		if update.Disposition != Pending {
			deleted, err := tx.ExecContext(ctx, `DELETE FROM pending_events WHERE event_id=?`, update.EventID)
			if err != nil {
				return Receipt{}, err
			}
			if affected, err := deleted.RowsAffected(); err != nil || affected != 1 {
				if err != nil {
					return Receipt{}, err
				}
				return Receipt{}, errors.New("projection pending buffer deletion diverged")
			}
		}
	}
	auditReason := projected.Reason
	if auditReason == "" {
		auditReason = strings.ToUpper(string(projected.Disposition))
	}
	if !auditReasonRE.MatchString(auditReason) {
		return Receipt{}, schema("E_STATE", "invalid audit reason")
	}
	metadata, err := json.Marshal(map[string]string{"digest": e.Digest})
	if err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(repository_id,disposition,reason_code,metadata,created_at) VALUES(?,?,?,?,?)`, repoID, string(projected.Disposition), auditReason, string(metadata), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Receipt{}, err
	}
	return Receipt{Disposition: projected.Disposition, Revision: rev, StateRevision: projected.Revision, Reason: projected.Reason}, tx.Commit()
}

// LifecycleProjection returns a copy of the bounded projection bytes and its
// reducer revision. The bytes are opaque to events.Store.
func (s *Store) LifecycleProjection(repositoryID string) ([]byte, int64, error) {
	var data []byte
	var revision int64
	err := s.db.QueryRow(`SELECT state,revision FROM lifecycle_projections WHERE repository_id=?`, repositoryID).Scan(&data, &revision)
	if err != nil {
		return nil, 0, err
	}
	return append([]byte(nil), data...), revision, nil
}

// Logs returns newest-first, redacted audit facts for one repository.
func (s *Store) Logs(ctx context.Context, repositoryID string, limit int) ([]AuditLog, error) {
	if repositoryID == "" {
		return nil, schema("E_SCOPE", "repository identity is required")
	}
	if !digestRE.MatchString(repositoryID) {
		return nil, schema("E_SCOPE", "invalid repository identity")
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, schema("E_USAGE", "log limit must be between 1 and 200")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.revision,a.disposition,a.reason_code,a.metadata,a.created_at,r.payload_digest FROM audit_events a LEFT JOIN event_receipts r ON r.revision=a.revision WHERE a.repository_id=? ORDER BY a.revision DESC LIMIT ?`, repositoryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]AuditLog, 0)
	for rows.Next() {
		var log AuditLog
		var metadata string
		var receiptDigest sql.NullString
		if err := rows.Scan(&log.Revision, &log.Disposition, &log.Reason, &metadata, &log.Timestamp, &receiptDigest); err != nil {
			return nil, err
		}
		if !receiptDigest.Valid || !auditDigestRE.MatchString(receiptDigest.String) {
			return nil, schema("E_STATE", "audit receipt is missing or invalid")
		}
		if log.Disposition == "" {
			// Legacy rows used reason_code for disposition.
			log.Disposition = log.Reason
		}
		if log.Reason == "" {
			log.Reason = strings.ToUpper(log.Disposition)
		}
		if log.Disposition != string(Accepted) && log.Disposition != string(Pending) && log.Disposition != string(Rejected) && log.Disposition != string(Duplicate) {
			return nil, schema("E_STATE", "invalid audit disposition")
		}
		if !auditReasonRE.MatchString(log.Reason) {
			return nil, schema("E_STATE", "missing audit reason")
		}
		if !strings.HasSuffix(log.Timestamp, "Z") {
			return nil, schema("E_STATE", "invalid audit timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, log.Timestamp); err != nil {
			return nil, schema("E_STATE", "invalid audit timestamp")
		}
		metadataDecoder := json.NewDecoder(strings.NewReader(metadata))
		metadataValue, metadataErr := decodeValue(metadataDecoder)
		if metadataErr != nil {
			return nil, schema("E_STATE", "invalid audit digest")
		}
		if token, tokenErr := metadataDecoder.Token(); tokenErr != io.EOF {
			_ = token
			return nil, schema("E_STATE", "invalid audit digest")
		}
		redacted, object := metadataValue.(map[string]any)
		if !object || len(redacted) != 1 {
			return nil, schema("E_STATE", "invalid audit digest")
		}
		digest, ok := redacted["digest"].(string)
		if !ok {
			return nil, schema("E_STATE", "invalid audit digest")
		}
		if !auditDigestRE.MatchString(digest) {
			return nil, schema("E_STATE", "invalid audit digest")
		}
		if digest != receiptDigest.String {
			return nil, schema("E_STATE", "audit digest does not match receipt")
		}
		log.EventDigest = digest
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// boundedPendingJSON retains only typed workflow facts needed for diagnostics
// and compatibility. Raw changes, paths, reasons, answers, extensions, and
// project context are deliberately excluded from the durable causal buffer.
func boundedPendingJSON(e Event) ([]byte, error) {
	payload := map[string]any{}
	for _, key := range []string{"status", "state", "outcome", "error_code", "prompt_id", "prompt_kind", "blocking", "candidate_revision", "base_head_digest", "tree_digest", "index_digest", "candidate_digest", "verification_id", "verifier_set", "verifier_version", "exit_code", "duration_ms", "commit_job_id", "commit_sha", "message_digest", "push_job_id", "remote_digest", "ref"} {
		if value, ok := e.Payload[key]; ok {
			payload[key] = value
		}
	}
	// Event is a typed Go value, so construct a bounded envelope explicitly
	// rather than serializing arbitrary ingress maps.
	scope := map[string]any{"repo_id": e.Scope["repo_id"]}
	for _, key := range []string{"worktree_id", "session_id", "task_id", "change_id"} {
		if value, ok := e.Scope[key]; ok {
			scope[key] = value
		}
	}
	ordering := map[string]any{"stream_id": e.Ordering["stream_id"]}
	for _, key := range []string{"producer_seq", "causation_id", "correlation_id"} {
		if value, ok := e.Ordering[key]; ok {
			ordering[key] = value
		}
	}
	capabilities := map[string]any{}
	for _, key := range []string{"queue_state", "task_boundaries", "changed_paths", "monotonic_sequence"} {
		if value, ok := e.Capabilities[key]; ok {
			capabilities[key] = value
		}
	}
	b := map[string]any{
		"schema_version": e.SchemaVersion, "event_class": e.EventClass,
		"event_id": e.EventID, "event_type": e.EventType, "occurred_at": e.OccurredAt,
		"producer": map[string]any{"kind": e.Producer["kind"], "adapter": e.Producer["adapter"], "version": e.Producer["version"], "installation_id": e.Producer["installation_id"], "instance_id": e.Producer["instance_id"]},
		"scope":    scope, "ordering": ordering, "idempotency": map[string]any{"key": e.Idempotency["key"]},
		"capabilities": capabilities, "payload": payload,
	}
	return json.Marshal(b)
}
func (s *Store) Accept(ctx context.Context, e Event) (Receipt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	var oldDigest, oldID string
	var rev int64
	key := stringValue(e.Idempotency["key"])
	idErr := tx.QueryRowContext(ctx, `SELECT event_id,payload_digest,revision FROM event_receipts WHERE event_id=?`, e.EventID).Scan(&oldID, &oldDigest, &rev)
	var keyDigest, keyID string
	var keyRev int64
	keyErr := tx.QueryRowContext(ctx, `SELECT event_id,payload_digest,revision FROM event_receipts WHERE idempotency_key=?`, key).Scan(&keyID, &keyDigest, &keyRev)
	if idErr == nil || keyErr == nil {
		if idErr == nil && keyErr == nil && oldID == e.EventID && keyID == e.EventID && oldDigest == e.Digest && keyDigest == e.Digest {
			return Receipt{Disposition: Duplicate, Revision: rev}, tx.Commit()
		}
		return Receipt{Disposition: Conflict, Reason: "E_IDEMPOTENCY_CONFLICT"}, schema("E_IDEMPOTENCY_CONFLICT", "event identity was reused with different content")
	}
	if !errors.Is(idErr, sql.ErrNoRows) || !errors.Is(keyErr, sql.ErrNoRows) {
		return Receipt{}, idErr
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM event_receipts`).Scan(&rev); err != nil {
		return Receipt{}, err
	}
	disp := Accepted
	causation := stringValue(e.Ordering["causation_id"])
	if causation != "" {
		var x string
		if err = tx.QueryRowContext(ctx, `SELECT event_id FROM event_receipts WHERE event_id=?`, causation).Scan(&x); errors.Is(err, sql.ErrNoRows) {
			disp = Pending
		} else if err != nil {
			return Receipt{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_receipts(event_id,idempotency_key,payload_digest,disposition,revision,created_at) VALUES(?,?,?,?,?,?)`, e.EventID, stringValue(e.Idempotency["key"]), e.Digest, disp, rev, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Receipt{}, err
	}
	if disp == Pending {
		b, _ := boundedPendingJSON(e)
		if _, err = tx.ExecContext(ctx, `INSERT INTO pending_events(event_id,causation_id,payload) VALUES(?,?,?)`, e.EventID, causation, b); err != nil {
			return Receipt{}, err
		}
	}
	metadata, err := json.Marshal(map[string]string{"digest": e.Digest})
	if err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(repository_id,disposition,reason_code,metadata,created_at) VALUES(?,?,?,?,?)`, stringValue(e.Scope["repo_id"]), string(disp), strings.ToUpper(string(disp)), string(metadata), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Receipt{}, err
	}
	return Receipt{Disposition: disp, Revision: rev}, tx.Commit()
}

// ReplayPending promotes all buffered events whose causal predecessor is now
// present. The payload is intentionally retained only until this transaction.
func (s *Store) ReplayPending(ctx context.Context, causationID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT event_id FROM pending_events WHERE causation_id=?`, causationID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE event_receipts SET disposition=? WHERE event_id=?`, Accepted, id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM pending_events WHERE event_id=?`, id); err != nil {
			return 0, err
		}
	}
	return len(ids), tx.Commit()
}

func (s *Store) ExpirePending(ctx context.Context, eventID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE event_receipts SET disposition=? WHERE event_id=? AND disposition=?`, Conflict, eventID, Pending)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM pending_events WHERE event_id=?`, eventID)
	return err
}
