# ADR-0009: Protected state filesystem boundary

- **Status:** Accepted for Phase 0
- **Context:** State databases, identity keys, policy, verifier configuration,
  and temporary replacements are security-sensitive. Path-string validation
  alone does not prevent traversal, final-component symlinks, permissive modes,
  or replacement during an atomic update.
- **Decision:** State-owned files use a canonical private root, reject
  traversal and symlinked final components, require regular files and
  restrictive modes, create identity keys exclusively, and replace policy and
  verifier files through fsynced temporary files followed by atomic rename.
  Trusted verifier reads revalidate the final file and bounded input before
  parsing.
- **Consequences:** Unsafe or corrupted state is visible as an error and does
  not get repaired implicitly. Read-only diagnostics may inspect a missing
  state root without creating it.
