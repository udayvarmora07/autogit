# ADR-0002: Invoke system Git

- **Status:** Proposed
- **Context:** Developers rely on credential helpers, signing, hooks, filters,
  attributes, worktrees, submodules, and Git LFS. Reimplementing Git semantics
  in a library would create behavioral and security differences.
- **Decision:** Invoke a supported system `git` executable with argument arrays,
  controlled environment, bounded output, explicit working directory, and no
  shell. Discover capabilities from the installed version and fail visibly when
  required features are unavailable. Persist a commit intent before invocation
  and reconcile `HEAD`, parent, tree, and message evidence after a crash; a
  lost process response must never cause an unverified duplicate commit.
- **Consequences:** AutoGit inherits mature Git behavior and user configuration,
  but must test multiple Git versions and treat hooks/config as side-effectful
  trust boundaries. Commands and exit statuses need structured translation;
  postconditions, not exit code alone, determine durable commit facts.
- **Alternatives considered:** `go-git` would reduce external-process calls but
  does not provide full parity with user Git behavior; embedding libgit2 adds
  native packaging and parity concerns.
