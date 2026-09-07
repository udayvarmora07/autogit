# AutoGit quickstart

The clean-machine path is intentionally explicit:

```sh
go build -o autogit ./cmd/autogit
./autogit version
./autogit doctor
./autogit init --repo /path/to/repository --local
./autogit status --repo /path/to/repository
```

`status`, `plan`, `logs`, `doctor`, `config explain`, and `integrity` are
read-only. A local commit requires an explicit session handoff and verifier
configuration (`sync --complete`); provider publication and public visibility
are separate consented commands. See `autogit help COMMAND` and
[`docs/release-runbook.md`](release-runbook.md) before enabling an adapter.
