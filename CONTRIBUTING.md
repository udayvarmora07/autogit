# Contributing to AutoGit

Keep changes narrow, evidence-backed, and compatible with AutoGit's safety
invariants. Do not use a real user repository, credential, provider account,
or source-bearing fixture in tests.

Before opening a change, run:

```sh
gofmt -w <changed-go-files>
bash scripts/check-shell.sh
bash scripts/test-suites.sh presubmit
go test ./...
```

Changes to durable state, Git/provider effects, adapters, release workflows,
or security controls need a regression test and an update to the relevant
plan/evidence document. Keep network and credentialed tests opt-in; the
default suite must be local and deterministic. Use the explicit `soak`,
`fuzz`, `canary`, and `release` suite names for their corresponding gates.

Commit messages should use a Conventional Commit prefix such as `fix:`,
`feat:`, `test:`, `docs:`, or `build:`. Pull requests should describe the
invariant protected, the failure mode tested, and the exact verification
commands run.
