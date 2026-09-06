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
- **Consequences:** A timeout returns a stable context error and leaves no
  running descendant process. Commands that exceed output limits fail closed.
  Platform support is tested through Linux descendant tests and Windows/macOS
  cross-builds; native platform execution remains a release evidence gate.
