# AutoGit v1 product requirements

Status: Draft for Phase 0 acceptance  
Last updated: 2026-09-01

## 1. Problem statement

Developers using AI agents across several CLIs and IDEs frequently forget to
initialize repositories, make meaningful commits, and push completed work.
Existing automation tends to commit an entire worktree at a tool stop event.
That can mix unrelated edits, publish incomplete code, leak credentials, create
low-quality history, or attach the wrong remote.

AutoGit must remove repetitive Git/GitHub work without removing the developer's
control over what is tracked, what is public, and what qualifies as complete.

## 2. Product goals

1. Ask once before tracking a project and retain a reversible decision.
2. Initialize Git and create a correctly owned GitHub repository safely.
3. Attribute changes to the agent session or explicitly approved user scope.
4. Commit logical completed tasks, not arbitrary tool or model stop events.
5. Generate accurate, useful Conventional Commit messages from intent, diff,
   and verification evidence.
6. Verify and scan changes before publication.
7. Work consistently across supported agentic CLIs, operating systems, and
   common project ecosystems.
8. Recover safely from duplicate events, crashes, authentication failures,
   network failures, and non-fast-forward pushes.
9. Help public repositories present credible engineering work rather than raw
   contribution volume.

## 3. Product principles

- Consent before mutation; explicit consent before public publication.
- Private by default; no implicit conversion from private to public.
- Preserve first, publish second.
- Fail closed for commit/push safety decisions and fail open only for agent
  availability: an AutoGit failure must not destroy work or trap the agent.
- Never force-push, rewrite shared history, or delete a remote repository.
- Never stage changes solely because they exist in the worktree.
- Verification evidence is bound to an immutable Git tree/diff hash.
- Local Git remains the source of truth; remote operations are resumable jobs.
- Hook payloads, prompts, paths, repository files, and configuration are
  untrusted inputs.
- Logs are useful without containing source, prompts, credentials, or full
  sensitive paths by default.

## 4. Actors

| Actor | Role |
| --- | --- |
| Developer | Owns consent, publication, policy, and conflict decisions. |
| Agentic client | Emits lifecycle/tool events and may identify changed files. |
| AutoGit adapter | Validates and normalizes one client's events. |
| AutoGit core | Owns state, policy, staging, verification, commit, and jobs. |
| System Git | Executes repository operations and honors user Git behavior. |
| GitHub CLI/provider | Authenticates, creates repositories, and publishes. |
| Verification tool | Produces bounded evidence for a specific tree hash. |

## 5. Functional requirements

### Consent and configuration

- **FR-CNS-001:** AutoGit MUST perform no Git initialization, commit, remote
  creation, or push until tracking consent exists for the resolved project.
- **FR-CNS-002:** Consent MUST record tracking mode, visibility, provider,
  destination owner, workflow mode, and policy version.
- **FR-CNS-003:** Public visibility MUST require an explicit choice and a
  pre-publication summary; omission means private.
- **FR-CNS-004:** A `no` decision MUST prevent future prompts until the user
  changes or removes the decision.
- **FR-CNS-005:** Project policy MUST override user defaults only for fields
  explicitly present and MUST never weaken mandatory safety invariants.
- **FR-CNS-006:** `enable`, `disable`, `status`, and `config explain` MUST make
  the effective decision inspectable and reversible.
- **FR-CNS-007:** `local-only` tracking MUST invoke no provider operation and
  MUST NOT push an existing remote; local commits may proceed without provider
  access.

### Repository discovery and creation

- **FR-REP-001:** AutoGit MUST resolve a canonical project root without acting
  on `/`, a home directory, an invalid symlink target, or an unapproved parent.
- **FR-REP-002:** Nested repositories, submodules, worktrees, bare repos, and
  linked worktrees MUST be detected before mutation.
- **FR-REP-003:** New repositories MUST use an explicit initial branch policy,
  generate/merge hygiene files without overwriting user content, and avoid an
  empty publication.
- **FR-REP-004:** GitHub repository creation MUST verify authenticated owner,
  final name, visibility, and returned canonical remote identity.
- **FR-REP-005:** A name collision MUST stop or select a user-approved new name;
  AutoGit MUST NOT attach a same-name remote as a fallback.
- **FR-REP-006:** Remote creation and local remote configuration MUST be a
  resumable transaction. Rollback may remove only an AutoGit-added local remote
  that has not been changed by the user.
- **FR-REP-007:** AutoGit MUST NOT delete a local or hosted repository.

### Sessions, tasks, and change ownership

- **FR-SES-001:** Every accepted event MUST belong to a stable repository,
  adapter, session, and event identity.
- **FR-SES-002:** AutoGit MUST capture a baseline at session or task start:
  `HEAD`, index tree, status, and pre-existing changed paths.
- **FR-SES-003:** A model/tool stop MUST be treated as a signal, not proof of
  task completion.
- **FR-SES-004:** Duplicate and out-of-order events MUST be idempotent and MUST
  not create duplicate commits or pushes.
- **FR-SES-005:** Concurrent sessions MUST have isolated candidate change sets
  and one serialized repository writer.
- **FR-SES-006:** If ownership of a path is ambiguous, it MUST be excluded or
  require explicit approval; it MUST NOT be guessed into a commit.
- **FR-SES-007:** A new event that changes the candidate tree MUST invalidate
  earlier verification and commit-message evidence.
- **FR-SES-008:** When queue/task information is unavailable, safe mode MUST
  wait for verified idle/settle policy or explicit completion; it MUST not
  infer completion from a single response stop.

### Staging and Git operations

- **FR-GIT-001:** Default staging MUST be session-owned, never unconditional
  whole-worktree staging.
- **FR-GIT-002:** Pre-existing staged and unstaged user changes MUST remain
  byte-for-byte and index-for-index unchanged unless explicitly included.
- **FR-GIT-003:** Renames, deletions, file mode changes, symlinks, submodules,
  Git LFS pointers, and case-only path changes MUST be represented correctly.
- **FR-GIT-004:** AutoGit MUST use argument-safe Git invocation without a shell
  and use pathspec termination/quoting appropriate to arbitrary valid paths.
- **FR-GIT-005:** Commit, push, and provider jobs MUST have durable idempotency
  keys derived from repository identity and immutable inputs.
- **FR-GIT-006:** AutoGit MUST NOT auto-force, auto-rebase, auto-reset, discard
  changes, or resolve conflicts without an explicit user action and policy.
- **FR-GIT-007:** Offline or failed pushes MUST leave the local commit intact and
  enqueue a visible retry without duplicating the commit.

### Ignore and file policy

- **FR-IGN-001:** AutoGit MUST consult Git ignore rules before proposing files.
- **FR-IGN-002:** Generated ignore rules MUST be based on detected ecosystems
  and trusted templates and MUST merge rather than overwrite.
- **FR-IGN-003:** Dependency lockfiles MUST be tracked by default when produced
  by the project, while dependencies/build outputs MUST be excluded by policy.
- **FR-IGN-004:** AutoGit-owned transient state MUST live outside the project or
  be excluded without rewriting unrelated ignore ordering/comments.

### Verification and security

- **FR-VER-001:** Verification MUST operate on or be bound to the exact
  candidate tree hash.
- **FR-VER-002:** Command selection MUST come from explicit configuration or a
  trusted detector; repository content MUST NOT silently introduce executable
  verification commands.
- **FR-VER-003:** Commands MUST have time, output, and cancellation limits and
  MUST preserve a structured result with redacted diagnostics.
- **FR-VER-004:** Safe publication MUST require all mandatory format, lint,
  type, test, build, and security checks configured for that project.
- **FR-VER-005:** If no meaningful verifier exists, safe mode MUST block public
  publication or request an explicit policy exception; it may create a local
  checkpoint.
- **FR-SEC-001:** Candidate content MUST be scanned for sensitive filenames,
  known secret signatures, private keys, high-entropy credentials, conflict
  markers, unsafe symlinks, disallowed binaries, and provider size limits.
- **FR-SEC-002:** Any scan result MUST identify a remediable path/category
  without printing the secret value.
- **FR-SEC-003:** Security exceptions MUST be narrow, recorded, time-bounded or
  content-hash-bound, and never global by accident.

### Commit composition

- **FR-CMT-001:** Messages MUST follow Conventional Commits by default:
  `type(scope): description`, with optional body/footer.
- **FR-CMT-002:** Message evidence MUST combine task intent, actual staged diff,
  repository convention, and verification result; filenames alone are not
  sufficient.
- **FR-CMT-003:** The subject MUST describe behavior/outcome, remain concise,
  and avoid timestamps, generic phrases, prompt text, secrets, and unsupported
  claims.
- **FR-CMT-004:** AutoGit MUST NOT invent authors, co-authors, signatures,
  issue closures, breaking changes, or provenance trailers.
- **FR-CMT-005:** One commit SHOULD represent one completed logical task. A
  mixed candidate MUST be split safely or require review.

### Publication and portfolio quality

- **FR-PUB-001:** Safe mode MUST publish a verified feature branch and may open
  a pull request; solo mode may push a verified current branch.
- **FR-PUB-002:** Protected-branch and status-check failures MUST redirect or
  block safely rather than bypass policy.
- **FR-PUB-003:** Before first public publication, AutoGit MUST present the
  visibility, file summary, scan results, verification results, README status,
  license status, and exact destination.
- **FR-PUB-004:** License selection MUST be explicit. AutoGit MUST NOT guess a
  license from repository visibility.
- **FR-PUB-005:** AutoGit SHOULD detect portfolio-readiness gaps such as a
  placeholder README, absent usage instructions, absent tests/CI, or missing
  description/topics without fabricating project details.

### Adapters, installation, and operations

- **FR-ADP-001:** Adapters MUST emit the versioned canonical event contract and
  MUST NOT perform Git mutation themselves.
- **FR-ADP-002:** The core MUST publish adapter capability requirements and
  safely degrade when a client lacks task, queue, or changed-file data.
- **FR-INS-001:** Installation MUST discover supported tools, back up configs,
  merge structurally, mark owned entries, validate them, and be idempotent.
- **FR-INS-002:** Upgrade/uninstall MUST alter only AutoGit-owned entries and
  preserve all unrelated formatting and configuration semantics where possible.
- **FR-OPS-001:** `doctor`, `status`, `plan`, `logs`, `retry`, and `explain`
  MUST report actionable state without exposing sensitive content.
- **FR-OPS-002:** AutoGit MUST notify the user when local work is committed but
  not published; silent success is prohibited for incomplete workflows.

## 6. Non-functional requirements

- **NFR-SAF-001:** No code path may delete a repository, force-push, discard a
  worktree/index change, or publish publicly without recorded consent.
- **NFR-REL-001:** Event processing and jobs are at-least-once and idempotent;
  recovery after process termination MUST not duplicate Git/provider effects.
- **NFR-PER-001:** With no candidate changes, hook handling SHOULD complete in
  under 150 ms at p95 on a typical local repository, excluding client startup.
- **NFR-PER-002:** Baseline/status analysis SHOULD complete under 1 second at
  p95 for 100,000 tracked paths, excluding Git LFS/network operations.
- **NFR-SEC-001:** Secrets MUST not appear in default logs, errors, database
  fields, telemetry, or commit messages.
- **NFR-PRV-001:** Telemetry MUST be opt-in and MUST not include prompts, source,
  diffs, file contents, repository remotes, or personally identifying paths.
- **NFR-POR-001:** Release binaries MUST support current stable Linux, macOS,
  and Windows on amd64 and arm64 where the toolchain supports them.
- **NFR-COM-001:** Adapter/event schemas MUST be versioned, validated, and
  backward compatible for the documented support window.
- **NFR-OBS-001:** Every state transition and external side effect MUST have a
  correlation ID and redacted structured audit event.
- **NFR-TST-001:** The core state machine, policy, and Git transaction layers
  MUST be deterministic and independently testable without a network.

## 7. Non-goals for v1

- Replacing Git, GitHub, or the user's credential manager.
- Generating or fixing application code to make tests pass.
- Automatically resolving merges/rebases or rewriting shared history.
- Publishing every prompt, intermediate thought, or tool call.
- Supporting every Git hosting provider in the first release.
- Measuring developer productivity through commit count.
- Acting as a backup for files that the user has excluded from Git.
- Cloud execution or a hosted source-code processing service.

## 8. Phase 0 exit criteria

- Requirements, threat model, event schema, lifecycle, and ADRs agree on
  terminology and safety invariants.
- Each must-level requirement has at least one planned acceptance test.
- Unknown capabilities for each initial adapter are recorded explicitly.
- Destructive and public-publication invariants have no undocumented override.
- Phase 1 implementation issues can be derived without a product-policy choice.
