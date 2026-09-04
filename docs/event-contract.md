# AutoGit v1 event contract

Status: Proposed for Phase 0  
Schema: [`schemas/event-v1.schema.json`](../schemas/event-v1.schema.json)  
Namespace: `autogit.event/1`

## Scope

The contract has two deliberately different classes of event:

* **Ingress events** are short-lived adapter observations from Codex, Claude
  Code, Cursor, Gemini CLI, OpenCode, or CommandCode. They are hints and never
  authorize a Git/provider side effect.
* **Domain events** are durable facts emitted by the AutoGit core after local
  inspection or a side effect. They are the only events used to rebuild state.

The adapter boundary is therefore not an event-sourcing shortcut: an adapter
reports what its client says, while the core independently inspects the
repository and records the resulting fact. The same envelope is used for both
classes so transport, deduplication, and compatibility are uniform.

## Transport and projection

The initial transport is one bounded UTF-8 JSON object on stdin and one JSON
result on stdout:

```text
autogit hook --adapter <adapter> --event <event-type>
```

The process must not invoke a shell. Human diagnostics go to stderr only when
the client hook can surface them. An ingress event may contain an ephemeral
`project` candidate (for example, the client cwd); the durable event receipt
must remove it after repository resolution. Raw adapter input is never stored
as a durable event or included in normal logs.

## Envelope

```json
{
  "schema_version": "autogit.event/1",
  "event_class": "ingress",
  "event_id": "01J7N6X8P5K2V4W6FQ8M9ABCDF",
  "event_type": "task.completed",
  "occurred_at": "2026-09-01T06:30:00Z",
  "producer": {
    "kind": "adapter",
    "adapter": "claude-code",
    "version": "1.2.0",
    "installation_id": "installation-id",
    "instance_id": "process-or-session-instance"
  },
  "scope": {
    "repo_id": "hmac-sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "worktree_id": "hmac-sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
    "session_id": "client-session-id",
    "task_id": "client-task-id"
  },
  "ordering": {
    "stream_id": "repo/worktree/session",
    "producer_seq": 18,
    "causation_id": "01J7N6X8P5K2V4W6FQ8M9ABCD0",
    "correlation_id": "01J7N70DB5A4J03GHN7CH5N2KR"
  },
  "idempotency": {
    "key": "adapter-operation-key",
    "attempt": 1
  },
  "capabilities": {
    "queue_state": "native",
    "task_boundaries": "native",
    "changed_paths": "reported",
    "monotonic_sequence": true
  },
  "project": {
    "candidate_root": "/workspace/example",
    "client_cwd": "/workspace/example/src"
  },
  "payload": {
    "status": "completed",
    "outcome": "success"
  },
  "extensions": {}
}
```

### Required and optional fields

Always required: `schema_version`, `event_class`, `event_id`, `event_type`,
`occurred_at`, `producer`, `scope.repo_id`, `ordering.stream_id`,
`idempotency.key`, and an object `payload`.

`producer.kind` is `adapter` for ingress and `core` or `reconciler` for domain
events. Adapter events require `producer.adapter`, `installation_id`, and
`instance_id`; domain events require `producer.instance_id` and may identify
the originating adapter in `producer.adapter`.

`session_id`, `task_id`, `change_id`, and `worktree_id` are conditional on the
event type. `producer_seq` is optional: adapters without ordering support must
omit it, never invent an empty queue or sequence. Domain events always carry a
`correlation_id` (the core generates one for a root operation); `causation_id`
is optional only for a root event and required for a derived event.
`capabilities`, `project`, and `extensions` are optional. `project` is
ingress-only and ephemeral.

IDs are opaque strings. AutoGit-generated IDs use UUIDv7/ULID; repository and
worktree identities are keyed HMAC digests of canonical local identities, not
raw paths. Timestamps are RFC 3339 UTC and are informational, never the sole
ordering mechanism.

## Event vocabulary

Ingress event types are:

```text
session.started       session.idle          session.ended
prompt.submitted      task.started          task.updated
task.completed        task.failed          tool.started
tool.completed        files.changed        model.stopped
```

Domain event types are:

```text
repository.discovered       policy.consent_requested
policy.set                  session.crashed
session.recovered           prompt.requested
prompt.queued               prompt.presented
prompt.answered             prompt.expired
prompt.cancelled            task.completion_candidate
task.completed              task.failed
task.cancelled              change.detected
change.staged               change.invalidated
verification.requested      verification.started
verification.passed         verification.failed
verification.invalidated    commit.requested
commit.created              commit.failed
commit.reconciled           push.requested
push.succeeded              push.failed
push.skipped
```

An ingress `task.completed` is a client claim. A domain `task.completed` is an
AutoGit fact emitted only after the configured completion and delivery
preconditions are satisfied. `model.stopped` is always a weak idle signal and
never completes a task.

Payload minimums:

| Event family | Required payload facts |
| --- | --- |
| Policy/consent | `prompt_id`, decision (`yes`/`no`), visibility, local-only and workflow flags where applicable |
| Prompt | `prompt_id`, prompt kind, blocking status; answer only on `prompt.answered` |
| Change | candidate revision, base `HEAD` digest, tree/index digest, bounded path count/digest |
| Verification | `verification_id`, verifier set/version, candidate digest, result, bounded exit/error data |
| Commit | `commit_job_id`, candidate/message digests; resulting SHA on success |
| Push | `push_job_id`, commit SHA, remote/ref identity digest, result/error code |
| Session/task | state or outcome, with explicit reason for failure/cancellation |

Core-generated sync and publication facts use the same minimums and add only
bounded evidence digests. Their event IDs are deterministic from repository,
event type, and idempotency key, so a retried CLI operation replays the same
fact instead of creating a second lifecycle transition. A publication fact may
be omitted for legacy/manual commits that have no projected lifecycle scope;
the durable push job remains authoritative for those operations.

For `session.started`, the core may include the bounded baseline facts
`baseline_head` (a Git object ID), `baseline_index`, `status_digest`, and
`baseline_paths_digest` (SHA-256 digests). Raw status output, paths, and file
contents are not event payload facts; the state store retains only these
identity/digest values.

Raw paths, diffs, source, command output, prompt text, credentials, tokens,
remote URLs, and unredacted commit messages are not payload facts.

## Results and errors

The response uses the same major-version convention:

```json
{
  "schema_version": "autogit.result/1",
  "event_id": "01J7N6X8P5K2V4W6FQ8M9ABCDF",
  "disposition": "accepted",
  "state_revision": 42,
  "action": "verify",
  "reason_code": "WAITING_FOR_VERIFICATION",
  "retryable": false
}
```

`disposition` is `accepted`, `duplicate`, `pending`, `rejected`, or
`unsupported`. `action` is `none`, `ask_consent`, `checkpoint`, `verify`,
`commit`, `push`, `notify`, or `blocked`. Human messages are diagnostics, not
API contracts. The adapter maps results to its own hook exit codes; AutoGit
does not assign cross-client meaning to exit codes.

Validation and processing rules:

1. Reject non-UTF-8, oversized, malformed, duplicate-key, or schema-invalid
   input with nonretryable `E_SCHEMA`; do not mutate Git, provider, or state.
2. Reject an unsupported major with `E_VERSION`; unknown minor fields under
   `extensions` are preserved/ignored. Unknown core envelope/payload fields
   are rejected rather than guessed.
3. Reject unknown event types with `E_EVENT_TYPE`; do not infer a mutation.
4. Persist `(event_id, canonical_payload_digest, idempotency.key)` before any
   side-effect scheduling. The same ID/key and digest returns the original
   result as `duplicate`. A changed digest returns `E_IDEMPOTENCY_CONFLICT` and
   is quarantined.
5. A valid event whose causal predecessor is missing returns `pending` and is
   durably buffered. It is replayed after the gap closes; an expired gap is
   quarantined as `E_CAUSAL_GAP`.
6. A valid but impossible transition is quarantined as `E_INVALID_TRANSITION`.
   Guard, verification, commit, and push failures are domain outcomes, not
   schema errors.
7. Unsafe roots, absolute/traversal paths, symlink escapes, or privacy policy
   violations return `E_SCOPE`/`E_PRIVACY` without fallback to `$PWD`, `$HOME`,
   or another implicit directory.

## Ordering, retries, and compatibility

Ordering is causal, not wall-clock based. Within a stream, `producer_seq` is a
useful hint; the durable core orders by causal references and local receipt
revision. Events with gaps remain pending. Late events are recorded and cause
fresh repository reconciliation; they cannot silently resurrect stale
verification.

`idempotency.key` identifies the logical operation, not an attempt. Retries
increment `attempt` while retaining the same key and payload digest. Side
effects use separate durable intent events (`commit.requested` and
`push.requested`) and are retried only when reconciliation proves the same
operation is safe.

The schema identity is `namespace/major`. Compatible optional additions retain
the major. New required fields, changed meanings, or removed enum values
require a new major. The core supports the current and previous major during a
documented migration window. Each adapter publishes a compatibility manifest
with supported majors, client versions, capabilities, and event mappings.

## Capability degradation

| Missing capability | Safe behavior |
| --- | --- |
| Task boundaries | Create a synthetic turn task; require explicit completion or configured settle evidence. |
| Queue state | Set `queue_state=none`; synthesize durable queue events and never treat unknown as empty. |
| Changed paths | Derive candidates from a baseline/current Git snapshot; exclude ambiguous pre-existing paths. |
| Monotonic sequence | Reconcile each completion/idle event; use event IDs and durable revisions. |
| Session identity | No automatic publication; allow explicit `autogit sync` or local checkpoint only. |

## Privacy and retention

The durable projection stores repository/worktree HMAC IDs, bounded metadata,
digests, result/error codes, and redacted audit records. Raw candidate roots may
be used transiently for resolution and then discarded. Logs default to IDs and
redacted basenames. State is encrypted/permission-restricted where supported,
has configurable retention, and records access to sensitive diagnostics.
