# ADR-0010: Bounded external process lifecycle

- **Status:** Accepted for Phase 0
- **Context:** Git, provider, and verifier commands can inherit hostile
  configuration, produce unbounded output, hang on prompts, or leave child
  processes after cancellation.
- **Decision:** External commands run through `internal/process` with an
  explicit argv, controlled environment, output cap, caller context, and
  platform process-tree lifecycle. Unix uses a dedicated process group;
  Windows uses a Job Object configured to terminate descendants when the job
  closes. CLI commands use one five-minute root deadline, while verifier and
  provider operations apply shorter operation-specific budgets.
- **Consequences:** A timeout returns a stable context error. Descendants that
  remain in the original process group or process tree are terminated. Commands
  that exceed output limits fail closed. Platform support is tested through
  Linux descendant tests and Windows/macOS cross-builds; native platform
  execution remains a release evidence gate.
- **Amendment (2026-10-16):** A descendant that deliberately starts a new
  session (for example `setsid`) escapes both the Unix process-group kill and
  the best-effort `/proc` descendant scan, so a repository-controlled helper
  can outlive verification in the process-bounded tier; the namespace tiers
  are required for hostile code that must not survive. Two bounds keep the
  boundary from hanging: output pipes held open by a detached descendant are
  force-closed after a short delay (`cmd.WaitDelay`), and that force-close is
  reported as an explicit failure (`exec.ErrWaitDelay`) rather than as a
  complete result, so callers fail closed instead of treating truncated
  output as verified. Regression coverage lives in
  `internal/process/process_detached_linux_test.go`.
