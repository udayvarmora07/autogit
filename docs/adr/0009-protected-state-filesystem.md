# ADR-0009: Protected state filesystem boundary

- **Status:** Accepted for Phase 0
- **Context:** State databases, identity keys, policy, verifier configuration,
  and temporary replacements are security-sensitive. Path-string validation
  alone does not prevent traversal, final-component symlinks, permissive modes,
  or replacement during an atomic update.
- **Decision:** State-owned files use an owned private root opened with
  `os.Root`; root ancestors, traversal, symlinked components, regular-file
  type, and restrictive modes are checked before access. Identity keys are
  created exclusively, while policy and verifier files use fsynced temporary
  files followed by root-relative atomic rename. Trusted verifier reads
  revalidate the final file and bounded input before parsing.
- **Consequences:** Unsafe or corrupted state is visible as an error and does
  not get repaired implicitly. Read-only diagnostics may inspect a missing
  state root without creating it.
