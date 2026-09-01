package adapters

// This file contains the adapter boundary only. Adapters decode untrusted
// client observations and produce data for the core; they intentionally have
// no Git, provider, or process-execution dependency.

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const maxInputBytes = 64 << 10

// unknownOccurredAt is a schema-valid sentinel used when a client hook does
// not report an event timestamp. Receipt time is deliberately excluded from
// canonical events so replaying the same raw observation cannot change its
// digest and trigger an idempotency conflict.
const unknownOccurredAt = "1970-01-01T00:00:00Z"

var (
	ErrSchema    = errors.New("invalid adapter schema")
	ErrEventType = errors.New("unknown event type")
	ErrVersion   = errors.New("unsupported adapter/event version")
	ErrScope     = errors.New("unsafe or unapproved scope")
	ErrAdapter   = errors.New("unknown adapter")
)

// Producer, Ordering, Idempotency, and CanonicalEvent mirror the stable
// envelope in schemas/event-v1.schema.json. Maps are used for scope/payload
// so the adapter package stays independent of the core's event store.
type Producer struct {
	Kind           string `json:"kind"`
	Adapter        string `json:"adapter"`
	Version        string `json:"version"`
	InstallationID string `json:"installation_id"`
	InstanceID     string `json:"instance_id"`
}

type Ordering struct {
	StreamID      string `json:"stream_id"`
	ProducerSeq   *int64 `json:"producer_seq,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

type Idempotency struct {
	Key     string `json:"key"`
	Attempt int    `json:"attempt,omitempty"`
}

type CapabilityManifest struct {
	Adapter           string            `json:"adapter"`
	SchemaMajors      []string          `json:"schema_majors"`
	ClientVersions    []string          `json:"client_versions,omitempty"`
	EventMappings     map[string]string `json:"event_mappings"`
	InstallSupported  bool              `json:"install_supported"`
	Contract          string            `json:"contract"`
	ResultExitCodes   map[string]int    `json:"result_exit_codes"`
	QueueState        string            `json:"queue_state"`
	TaskBoundaries    string            `json:"task_boundaries"`
	ChangedPaths      string            `json:"changed_paths"`
	MonotonicSequence bool              `json:"monotonic_sequence"`
}

type CanonicalEvent struct {
	SchemaVersion string            `json:"schema_version"`
	EventClass    string            `json:"event_class"`
	EventID       string            `json:"event_id"`
	EventType     string            `json:"event_type"`
	OccurredAt    string            `json:"occurred_at"`
	Producer      Producer          `json:"producer"`
	Scope         map[string]string `json:"scope"`
	Ordering      Ordering          `json:"ordering"`
	Idempotency   Idempotency       `json:"idempotency"`
	Capabilities  *Capabilities     `json:"capabilities,omitempty"`
	Project       map[string]string `json:"project,omitempty"`
	Payload       map[string]any    `json:"payload"`
	Extensions    map[string]any    `json:"extensions,omitempty"`
}

// Capabilities is the wire representation. CapabilityManifest is the public
// compatibility document and includes adapter/version support metadata.
type Capabilities struct {
	QueueState        string `json:"queue_state,omitempty"`
	TaskBoundaries    string `json:"task_boundaries,omitempty"`
	ChangedPaths      string `json:"changed_paths,omitempty"`
	MonotonicSequence bool   `json:"monotonic_sequence"`
}

type TranslateOptions struct {
	ApprovedRoots []string
	// ResolvedScope is supplied by the trusted core after repository
	// discovery; raw client repository identifiers are never authoritative.
	ResolvedScope  map[string]string
	InstallationID string
	InstanceID     string
	ClientVersion  string
	// EventHint is supplied by a trusted installed command (for example,
	// --event session.ended). It takes precedence over an untrusted or
	// client-specific event field in the hook payload.
	EventHint string
}

type Result struct {
	Disposition string
	ReasonCode  string
	Retryable   bool
}

type ExitMapping struct {
	ExitCode  int
	Retryable bool
}

type Adapter interface {
	Name() string
	Manifest() CapabilityManifest
	Translate(raw []byte, options TranslateOptions) (CanonicalEvent, error)
	MapResult(Result) ExitMapping
}

type adapter struct {
	name         string
	manifest     CapabilityManifest
	capabilities Capabilities
}

var capabilityTable = map[string]Capabilities{
	"codex":       {QueueState: "unknown", TaskBoundaries: "native", ChangedPaths: "reported", MonotonicSequence: true},
	"claude-code": {QueueState: "unknown", TaskBoundaries: "native", ChangedPaths: "reported", MonotonicSequence: true},
	"cursor":      {QueueState: "none", TaskBoundaries: "synthetic", ChangedPaths: "none"},
	"gemini-cli":  {QueueState: "unknown", TaskBoundaries: "native", ChangedPaths: "reported", MonotonicSequence: true},
	"opencode":    {QueueState: "none", TaskBoundaries: "synthetic", ChangedPaths: "derived"},
	"commandcode": {QueueState: "unknown", TaskBoundaries: "synthetic", ChangedPaths: "derived", MonotonicSequence: false},
}

// clientContracts is deliberately explicit. A client whose hook/API contract
// is not stable is represented as an observation-only adapter; installation
// is owned by the install package and is never implied by this metadata.
var clientContracts = map[string]struct {
	versions []string
	mapping  map[string]string
	install  bool
	contract string
}{
	"codex":       {versions: []string{"unknown", "0.x", "1.x", "2.x"}, mapping: map[string]string{"hook_event_name": "event", "session_id": "scope.session_id", "task_id": "scope.task_id"}, install: true, contract: "official-hook"},
	"claude-code": {versions: []string{"unknown", "1.x", "2.x"}, mapping: map[string]string{"hook_event_name": "event", "session_id": "scope.session_id", "cwd": "project.candidate_root"}, install: true, contract: "official-hook"},
	"cursor":      {versions: []string{"observation"}, mapping: map[string]string{"observation": "event", "sessionId": "scope.session_id"}, contract: "synthetic-observation"},
	"gemini-cli":  {versions: []string{"unknown", "0.x", "1.x", "2.x"}, mapping: map[string]string{"hook_event_name": "event", "session_id": "scope.session_id"}, install: true, contract: "official-hook"},
	"opencode":    {versions: []string{"observation"}, mapping: map[string]string{"observation": "event", "session": "scope.session_id"}, contract: "synthetic-observation"},
	"commandcode": {versions: []string{"observation"}, mapping: map[string]string{"signal": "event", "session_id": "scope.session_id"}, contract: "synthetic-observation"},
}

func SupportedNames() []string {
	return []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "commandcode"}
}

func New(name string) (Adapter, error) {
	cap, ok := capabilityTable[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAdapter, name)
	}
	return &adapter{name: name, capabilities: cap, manifest: CapabilityManifest{
		Adapter: name, SchemaMajors: []string{"autogit.event/1"}, QueueState: cap.QueueState,
		TaskBoundaries: cap.TaskBoundaries, ChangedPaths: cap.ChangedPaths,
		MonotonicSequence: cap.MonotonicSequence,
		ClientVersions:    append([]string(nil), clientContracts[name].versions...),
		EventMappings:     cloneStringMap(clientContracts[name].mapping), InstallSupported: clientContracts[name].install,
		Contract:        clientContracts[name].contract,
		ResultExitCodes: map[string]int{"accepted": 0, "duplicate": 0, "pending": 75, "unsupported": 78, "rejected": 1},
	}}, nil
}

// NewAdapter is an explicit alias useful to callers that prefer a named
// constructor.
func NewAdapter(name string) (Adapter, error) { return New(name) }

// ManifestFor returns the complete compatibility document, including the
// safe-degradation capabilities used on the wire.
func ManifestFor(name string) (CapabilityManifest, error) {
	a, err := New(name)
	if err != nil {
		return CapabilityManifest{}, err
	}
	return a.Manifest(), nil
}

func cloneCapabilityManifest(in CapabilityManifest) CapabilityManifest {
	out := in
	out.SchemaMajors = append([]string(nil), in.SchemaMajors...)
	out.ClientVersions = append([]string(nil), in.ClientVersions...)
	out.EventMappings = cloneStringMap(in.EventMappings)
	out.ResultExitCodes = make(map[string]int, len(in.ResultExitCodes))
	for key, value := range in.ResultExitCodes {
		out.ResultExitCodes[key] = value
	}
	return out
}

func (a *adapter) Name() string                 { return a.name }
func (a *adapter) Manifest() CapabilityManifest { return cloneCapabilityManifest(a.manifest) }
func (a *adapter) MapResult(r Result) ExitMapping {
	code, ok := a.manifest.ResultExitCodes[r.Disposition]
	if !ok {
		code = 1
	}
	return ExitMapping{ExitCode: code, Retryable: r.Disposition == "pending" && r.Retryable}
}

var eventAliases = map[string]string{
	"start": "session.started", "session.start": "session.started", "session_started": "session.started", "session.started": "session.started",
	"idle": "session.idle", "session.idle": "session.idle", "session_idle": "session.idle",
	"end": "session.ended", "session.end": "session.ended", "session.ended": "session.ended", "stop": "model.stopped", "model.stop": "model.stopped", "model.stopped": "model.stopped",
	"sessionend": "session.ended", "session_end": "session.ended", "session-end": "session.ended",
	"prompt": "prompt.submitted", "prompt.submit": "prompt.submitted", "prompt.submitted": "prompt.submitted",
	"task.start": "task.started", "task.started": "task.started", "task.update": "task.updated", "task.updated": "task.updated",
	"complete": "task.completed", "completed": "task.completed", "task.complete": "task.completed", "task.completed": "task.completed",
	"fail": "task.failed", "failed": "task.failed", "task.fail": "task.failed", "task.failed": "task.failed",
	"tool.start": "tool.started", "tool.started": "tool.started", "tool.complete": "tool.completed", "tool.completed": "tool.completed",
	"change": "files.changed", "files.change": "files.changed", "files.changed": "files.changed",
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *adapter) eventValue(m map[string]any) string {
	// Ordered per-client candidates preserve the actual hook/API contract and
	// avoid treating an unrelated generic field as authoritative.
	var keys []string
	switch a.name {
	case "codex", "gemini-cli":
		keys = []string{"hook_event_name", "hook", "event", "event_type", "type"}
	case "claude-code":
		keys = []string{"hook_event_name", "eventName", "event", "hook", "type"}
	case "cursor", "opencode":
		keys = []string{"observation", "event", "event_type", "hook", "type"}
	case "commandcode":
		keys = []string{"signal", "event", "event_type", "hook", "type"}
	}
	return firstString(m, keys...)
}

func (a *adapter) field(m map[string]any, key string) string {
	switch a.name {
	case "cursor":
		if key == "session" {
			return firstString(m, "sessionId", "session_id", "session")
		}
	case "opencode":
		if key == "session" {
			return firstString(m, "session", "session_id", "sessionId")
		}
	}
	return firstString(m, key, key+"_id", key+"Id")
}

func supportedClientVersion(version string) bool {
	version = strings.TrimSpace(strings.ToLower(version))
	if version == "" || version == "unknown" {
		return true
	}
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return false
	}
	major := 0
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return false
		}
		major = major*10 + int(r-'0')
		if major > 99 {
			return false
		}
	}
	return major <= 2
}

func capabilitiesFromObservation(m map[string]any, base Capabilities) Capabilities {
	raw, ok := m["capabilities"].(map[string]any)
	if !ok {
		if _, present := m["capabilities"]; present {
			return Capabilities{QueueState: "unknown", TaskBoundaries: "synthetic", ChangedPaths: "derived"}
		}
		return base
	}
	// An explicitly partial capability object is not permission to infer the
	// omitted guarantees. Unknown/derived values force reconciliation in core.
	cap := Capabilities{QueueState: "unknown", TaskBoundaries: "synthetic", ChangedPaths: "derived"}
	if value, ok := raw["queue_state"].(string); ok && value != "" {
		cap.QueueState = value
	}
	if value, ok := raw["task_boundaries"].(string); ok && value != "" {
		cap.TaskBoundaries = value
	}
	if value, ok := raw["changed_paths"].(string); ok && value != "" {
		cap.ChangedPaths = value
	}
	if value, ok := raw["monotonic_sequence"].(bool); ok {
		cap.MonotonicSequence = value
	}
	return cap
}

func validateDeclaredVersion(m map[string]any) error {
	for _, key := range []string{"schema_version", "event_schema_version"} {
		if value, exists := m[key]; exists {
			if _, ok := value.(string); !ok {
				return ErrSchema
			}
		}
	}
	value := firstString(m, "schema_version", "event_schema_version")
	if value == "" {
		return nil
	}
	if value != "autogit.event/1" {
		return fmt.Errorf("%w: %s", ErrVersion, value)
	}
	return nil
}

func hasAnyKey(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func validateScalarFields(m map[string]any, keys ...string) error {
	for _, key := range keys {
		value, exists := m[key]
		if !exists || value == nil {
			continue
		}
		switch key {
		case "producer_seq", "sequence", "seq", "attempt", "retry":
			if _, ok := integer(map[string]any{key: value}, key); !ok {
				return fmt.Errorf("%w: %s must be an integer", ErrSchema, key)
			}
		default:
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%w: %s must be a string", ErrSchema, key)
			}
		}
	}
	return nil
}

func (a *adapter) Translate(raw []byte, opts TranslateOptions) (CanonicalEvent, error) {
	if len(raw) == 0 || len(raw) > maxInputBytes || !utf8.Valid(raw) {
		return CanonicalEvent{}, ErrSchema
	}
	v, err := decodeStrict(raw)
	if err != nil {
		return CanonicalEvent{}, fmt.Errorf("%w: %v", ErrSchema, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return CanonicalEvent{}, ErrSchema
	}
	if err := validateDeclaredVersion(m); err != nil {
		return CanonicalEvent{}, err
	}
	if err := validateScalarFields(m, "event", "event_type", "type", "hook", "eventName", "hook_event_name", "observation", "signal", "occurred_at", "timestamp", "time", "producer_seq", "sequence", "seq", "attempt", "retry"); err != nil {
		return CanonicalEvent{}, err
	}
	eventRaw := opts.EventHint
	if eventRaw == "" {
		eventRaw = a.eventValue(m)
	}
	eventType, ok := eventAliases[strings.ToLower(strings.TrimSpace(eventRaw))]
	if !ok {
		return CanonicalEvent{}, fmt.Errorf("%w: %q", ErrEventType, eventRaw)
	}
	if err := validateRoot(m, opts); err != nil {
		return CanonicalEvent{}, err
	}
	installation := opts.InstallationID
	if installation == "" {
		installation = firstString(m, "installation_id", "installation")
	}
	if installation == "" {
		installation = "adapter-" + a.name
	}
	instance := opts.InstanceID
	if instance == "" {
		instance = firstString(m, "instance_id", "instance", "process_id")
	}
	if instance == "" {
		instance = stableID(a.name + "/" + firstString(m, "session_id", "session"))
	}
	clientVersion := opts.ClientVersion
	explicitClientVersion := clientVersion != ""
	if clientVersion == "" {
		clientVersion = firstString(m, "client_version", "version")
		explicitClientVersion = clientVersion != ""
	}
	if clientVersion == "" {
		clientVersion = "unknown"
	}

	root := firstString(m, "candidate_root", "cwd", "client_cwd")
	if p, ok := m["project"].(map[string]any); ok {
		if root == "" {
			root = firstString(p, "candidate_root", "cwd", "client_cwd")
		}
	}
	project := map[string]string{}
	if root != "" {
		clean, err := approvedRoot(root, opts.ApprovedRoots)
		if err != nil {
			return CanonicalEvent{}, err
		}
		project["candidate_root"] = clean
		project["client_cwd"] = clean
	}

	sessionID := a.field(m, "session")
	taskID := a.field(m, "task")
	if needsSession(eventType) && sessionID == "" {
		return CanonicalEvent{}, fmt.Errorf("%w: missing session_id", ErrSchema)
	}
	if needsTask(eventType) && taskID == "" {
		return CanonicalEvent{}, fmt.Errorf("%w: missing task_id", ErrSchema)
	}
	if (sessionID != "" && !validID(sessionID)) || (taskID != "" && !validID(taskID)) {
		return CanonicalEvent{}, fmt.Errorf("%w: invalid session/task identity", ErrSchema)
	}
	repoID := opts.ResolvedScope["repo_id"]
	if !validDigest(repoID) {
		return CanonicalEvent{}, fmt.Errorf("%w: missing trusted repository scope", ErrScope)
	}
	worktreeID := opts.ResolvedScope["worktree_id"]
	if worktreeID == "" {
		worktreeID = opts.ResolvedScope["worktree"]
	}
	if worktreeID != "" && !validDigest(worktreeID) {
		return CanonicalEvent{}, fmt.Errorf("%w: invalid trusted worktree scope", ErrScope)
	}
	streamID := firstString(m, "stream_id", "stream", "ordering_stream")
	if streamID == "" {
		streamID = repoID + "/" + sessionID
	}
	if streamID == "/" || strings.ContainsAny(streamID, "\x00\r\n") {
		return CanonicalEvent{}, ErrScope
	}
	seq, hasSeq := integer(m, "producer_seq", "sequence", "seq")
	if hasAnyKey(m, "producer_seq", "sequence", "seq") {
		if !hasSeq || seq < 0 {
			return CanonicalEvent{}, ErrSchema
		}
	}

	payload := canonicalPayload(m, eventType)
	if err := validateCanonicalRequirements(eventType, payload, scopeValues(sessionID, taskID, worktreeID, firstString(m, "change_id", "candidate_id"))); err != nil {
		return CanonicalEvent{}, err
	}
	if eventType == "files.changed" {
		changes, ok := payload["changes"].([]map[string]any)
		if !ok || len(changes) == 0 {
			return CanonicalEvent{}, fmt.Errorf("%w: files.changed requires changes", ErrSchema)
		}
		for _, change := range changes {
			path, _ := change["path"].(string)
			if !safeRelativePath(path) {
				return CanonicalEvent{}, ErrScope
			}
		}
	}
	eventID := firstString(m, "event_id", "id")
	sourceIdentity := firstString(m, "event_id", "id", "idempotency_key", "operation_id")
	if sourceIdentity == "" {
		if hasSeq {
			sourceIdentity = fmt.Sprintf("seq-%d", seq)
		} else {
			// Several official stop/session-end hooks do not carry an event ID.
			// Derive one from the complete decoded observation (JSON object keys
			// are encoded canonically by encoding/json), never from wall-clock
			// time or a process-global counter.
			canonical, _ := json.Marshal(m)
			sourceIdentity = "observation-" + stableID(a.name+"/"+string(canonical))
		}
	}
	// Client IDs are opaque and are not required to be ULIDs. Preserve a
	// canonical ULID only when the client supplied one; otherwise derive one
	// deterministically from adapter and source identity.
	if !validULID(eventID) {
		eventID = stableID(a.name + "/event/" + sourceIdentity)
	}
	idempotency := firstString(m, "idempotency_key", "idempotency", "operation_id")
	if idempotency == "" {
		idempotency = stableID(a.name + "/operation/" + sourceIdentity)
	}
	if !validID(idempotency) {
		idempotency = stableID(a.name + "/operation/" + idempotency)
	}
	occurred := firstString(m, "occurred_at", "timestamp", "time")
	if occurred == "" {
		occurred = unknownOccurredAt
	}
	parsed, err := time.Parse(time.RFC3339, occurred)
	if err != nil || !strings.HasSuffix(occurred, "Z") {
		return CanonicalEvent{}, fmt.Errorf("%w: occurred_at", ErrSchema)
	}
	ordering := Ordering{StreamID: streamID}
	if hasSeq {
		ordering.ProducerSeq = &seq
	}
	ordering.CausationID = firstString(m, "causation_id", "caused_by")
	ordering.CorrelationID = firstString(m, "correlation_id", "correlation")
	if ordering.CausationID != "" && !validULID(ordering.CausationID) {
		return CanonicalEvent{}, ErrSchema
	}
	if ordering.CorrelationID != "" && !validID(ordering.CorrelationID) {
		return CanonicalEvent{}, ErrSchema
	}
	if sessionID != "" { /* scope fields below */
	}
	scope := map[string]string{"repo_id": repoID}
	if worktreeID != "" {
		scope["worktree_id"] = worktreeID
	}
	if sessionID != "" {
		scope["session_id"] = sessionID
	}
	if taskID != "" {
		scope["task_id"] = taskID
	}
	if changeID := firstString(m, "change_id", "candidate_id"); changeID != "" {
		scope["change_id"] = changeID
	}
	if err := validateScopeIDs(scope); err != nil {
		return CanonicalEvent{}, err
	}
	capabilities := capabilitiesFromObservation(m, a.capabilities)
	if explicitClientVersion && !supportedClientVersion(clientVersion) {
		// Unknown client versions are observations only. Never infer queue
		// emptiness, task boundaries, changed paths, or ordering guarantees.
		capabilities = Capabilities{QueueState: "unknown", TaskBoundaries: "synthetic", ChangedPaths: "derived", MonotonicSequence: false}
	}
	return CanonicalEvent{SchemaVersion: "autogit.event/1", EventClass: "ingress", EventID: eventID,
		EventType: eventType, OccurredAt: parsed.UTC().Format(time.RFC3339),
		Producer: Producer{Kind: "adapter", Adapter: a.name, Version: clientVersion, InstallationID: installation, InstanceID: instance},
		Scope:    scope, Ordering: ordering, Idempotency: Idempotency{Key: idempotency, Attempt: attempt(m)},
		Capabilities: &capabilities, Project: projectOrNil(project), Payload: payload}, nil
}

func needsSession(event string) bool {
	return strings.HasPrefix(event, "session.") || strings.HasPrefix(event, "prompt.") || strings.HasPrefix(event, "task.") || strings.HasPrefix(event, "tool.")
}
func needsTask(event string) bool {
	return strings.HasPrefix(event, "prompt.") || strings.HasPrefix(event, "task.") || strings.HasPrefix(event, "tool.")
}

func scopeValues(session, task, worktree, change string) map[string]string {
	return map[string]string{"session_id": session, "task_id": task, "worktree_id": worktree, "change_id": change}
}

func validateScopeIDs(scope map[string]string) error {
	for key, value := range scope {
		if value == "" {
			continue
		}
		if key == "worktree_id" {
			if !validDigest(value) {
				return fmt.Errorf("%w: invalid %s", ErrScope, key)
			}
			continue
		}
		if !validID(value) {
			return fmt.Errorf("%w: invalid %s", ErrSchema, key)
		}
	}
	return nil
}

func validateCanonicalRequirements(event string, p map[string]any, scope map[string]string) error {
	if (strings.HasPrefix(event, "change.") || strings.HasPrefix(event, "verification.") || strings.HasPrefix(event, "commit.") || strings.HasPrefix(event, "push.")) && (scope["worktree_id"] == "" || scope["session_id"] == "" || scope["task_id"] == "" || scope["change_id"] == "") {
		return fmt.Errorf("%w: change event requires complete trusted scope", ErrSchema)
	}
	if strings.HasPrefix(event, "prompt.") {
		if !validID(fmt.Sprint(p["prompt_id"])) || p["prompt_kind"] == nil {
			return fmt.Errorf("%w: prompt facts required", ErrSchema)
		}
	}
	if strings.HasPrefix(event, "verification.") && (!validID(fmt.Sprint(p["verification_id"])) || !validDigest(fmt.Sprint(p["candidate_digest"]))) {
		return fmt.Errorf("%w: verification facts required", ErrSchema)
	}
	if strings.HasPrefix(event, "commit.") && (!validID(fmt.Sprint(p["commit_job_id"])) || !validDigest(fmt.Sprint(p["candidate_digest"]))) {
		return fmt.Errorf("%w: commit facts required", ErrSchema)
	}
	if strings.HasPrefix(event, "push.") && (!validID(fmt.Sprint(p["push_job_id"])) || !validGitSHA(fmt.Sprint(p["commit_sha"]))) {
		return fmt.Errorf("%w: push facts required", ErrSchema)
	}
	if outcome, ok := p["outcome"].(string); ok && outcome != "success" && outcome != "failure" && outcome != "cancelled" && outcome != "unknown" {
		return fmt.Errorf("%w: invalid outcome", ErrSchema)
	}
	return nil
}

func validGitSHA(s string) bool {
	if len(s) < 40 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validateRoot(m map[string]any, opts TranslateOptions) error {
	root := firstString(m, "candidate_root", "cwd", "client_cwd")
	if p, ok := m["project"].(map[string]any); ok && root == "" {
		root = firstString(p, "candidate_root", "client_cwd")
	}
	if root == "" {
		return nil
	}
	if len(opts.ApprovedRoots) == 0 {
		return ErrScope
	}
	return nil
}

func approvedRoot(root string, approved []string) (string, error) {
	if root == "" || strings.ContainsRune(root, '\x00') || !filepath.IsAbs(root) {
		return "", ErrScope
	}
	clean := filepath.Clean(root)
	if clean == string(filepath.Separator) {
		return "", ErrScope
	}
	for _, base := range approved {
		if base == "" || !filepath.IsAbs(base) {
			continue
		}
		b := filepath.Clean(base)
		rel, err := filepath.Rel(b, clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if real, err := filepath.EvalSymlinks(clean); err == nil {
				if realRel, e := filepath.Rel(b, real); e != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
					return "", ErrScope
				}
			}
			return clean, nil
		}
	}
	return "", ErrScope
}

func canonicalPayload(m map[string]any, event string) map[string]any {
	p := map[string]any{}
	if nested, ok := m["payload"].(map[string]any); ok {
		for k, v := range nested {
			addPayload(p, k, v)
		}
	}
	for _, k := range []string{"status", "state", "outcome", "reason", "error_code", "prompt_id", "prompt_kind", "blocking", "answer", "candidate_revision", "base_head_digest", "tree_digest", "index_digest", "candidate_digest", "verification_id", "verifier_set", "verifier_version", "exit_code", "duration_ms", "commit_job_id", "commit_sha", "message_digest", "push_job_id", "remote_digest", "ref"} {
		if v, ok := m[k]; ok {
			addPayload(p, k, v)
		}
	}
	if _, ok := p["outcome"]; !ok {
		status := strings.ToLower(fmt.Sprint(p["status"]))
		switch {
		case strings.Contains(status, "success"), status == "completed", status == "done":
			p["outcome"] = "success"
		case strings.Contains(status, "fail"), status == "error":
			p["outcome"] = "failure"
		case strings.Contains(status, "cancel"):
			p["outcome"] = "cancelled"
		}
	}
	if event == "files.changed" || firstAny(m, "changed_files", "changedPaths", "files") != nil {
		p["changes"] = changes(m)
	}
	return p
}

func addPayload(p map[string]any, key string, value any) {
	if key == "changed_paths" || key == "changed_files" {
		key = "changes"
	}
	switch key {
	case "status", "state", "outcome", "reason", "error_code", "prompt_id", "prompt_kind", "blocking", "answer", "changes", "candidate_revision", "base_head_digest", "tree_digest", "index_digest", "candidate_digest", "verification_id", "verifier_set", "verifier_version", "exit_code", "duration_ms", "commit_job_id", "commit_sha", "message_digest", "push_job_id", "remote_digest", "ref":
		p[key] = value
	}
}

func changes(m map[string]any) []map[string]any {
	value := firstAny(m, "changed_files", "changedPaths", "files", "changes")
	if nested, ok := m["payload"].(map[string]any); ok && value == nil {
		value = firstAny(nested, "changed_files", "changedPaths", "files", "changes")
	}
	var out []map[string]any
	switch values := value.(type) {
	case []any:
		for _, v := range values {
			switch x := v.(type) {
			case string:
				out = append(out, map[string]any{"path": x, "operation": "modified"})
			case map[string]any:
				path := firstString(x, "path", "file", "name")
				if path != "" {
					op := firstString(x, "operation", "status", "kind")
					if op == "" {
						op = "modified"
					}
					out = append(out, map[string]any{"path": path, "operation": normalizeOperation(op)})
				}
			}
		}
	case []map[string]any:
		for _, x := range values {
			out = append(out, x)
		}
	}
	return out
}

func normalizeOperation(s string) string {
	s = strings.ToLower(s)
	switch s {
	case "a", "add", "added":
		return "added"
	case "d", "delete", "deleted":
		return "deleted"
	case "r", "rename", "renamed":
		return "renamed"
	case "mode":
		return "mode-changed"
	default:
		return "modified"
	}
}
func projectOrNil(p map[string]string) map[string]string {
	if len(p) == 0 {
		return nil
	}
	return p
}
func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
func integer(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		switch n := m[k].(type) {
		case json.Number:
			v, e := n.Int64()
			return v, e == nil
		case float64:
			return int64(n), n == float64(int64(n))
		case int64:
			return n, true
		}
	}
	return 0, false
}
func attempt(m map[string]any) int {
	n, ok := integer(m, "attempt", "retry")
	if !ok || n < 1 {
		return 1
	}
	if n > 100000 {
		return 100000
	}
	return int(n)
}
func digestIdentity(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return "sha256:" + fmt.Sprintf("%x", h[:])
}

// CanonicalDigest returns the stable receipt digest for an adapter event.
// encoding/json sorts map keys, so equivalent decoded observations produce
// identical digests regardless of client key order.
func CanonicalDigest(event CanonicalEvent) (string, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	return digestIdentity(string(raw)), nil
}
func validID(s string) bool {
	if s == "" || len(s) > 256 || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/-", r)) {
			return false
		}
	}
	return true
}
func validULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'H' || r >= 'J' && r <= 'K' || r >= 'M' && r <= 'N' || r >= 'P' && r <= 'T' || r >= 'V' && r <= 'Z') {
			return false
		}
	}
	return true
}
func validDigest(s string) bool {
	sep := strings.IndexByte(s, ':')
	if sep < 1 || (s[:sep] != "sha256" && s[:sep] != "hmac-sha256") {
		return false
	}
	return isLowerHex(s[sep+1:])
}
func isLowerHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func safeRelativePath(path string) bool {
	if path == "" || strings.ContainsAny(path, "\\\x00\r\n") || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && clean == path
}
func stableID(s string) string {
	h := sha256.Sum256([]byte(s))
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:])
	enc = strings.ToUpper(enc)
	enc = strings.NewReplacer("I", "H", "L", "K", "O", "N", "U", "V").Replace(enc)
	return enc[:26]
}

// decodeStrict rejects duplicate object keys, trailing data, and non-object
// values. Decoder.UseNumber avoids silently rounding large sequence numbers.
func decodeStrict(raw []byte) (any, error) {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	v, err := parseJSONValue(d)
	if err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing data")
		}
		return nil, err
	}
	return v, nil
}
func parseJSONValue(d *json.Decoder) (any, error) {
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
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, ok := key.(string)
				if !ok {
					return nil, errors.New("object key")
				}
				if _, exists := m[k]; exists {
					return nil, fmt.Errorf("duplicate key %q", k)
				}
				v, err := parseJSONValue(d)
				if err != nil {
					return nil, err
				}
				m[k] = v
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return m, nil
		case '[':
			var a []any
			for d.More() {
				v, err := parseJSONValue(d)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return a, nil
		}
	}
	return t, nil
}
