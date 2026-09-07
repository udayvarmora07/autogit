# P2-11 and Phase 3 implementation evidence

Date: 2026-09-07  
Scope: P2-11, P3-01 through P3-09  
Status: local implementation complete; native/release gates remain separate

## Delivered controls

| ID | Evidence |
| --- | --- |
| P2-11 | `internal/mcp` implements JSON-RPC 2.0 stdio with protocol revision `2025-06-18`, bounded input/rate, `tools/list`, `tools/call`, lifecycle responses, and four read-only tools. `cmd/autogit mcp serve` wires status, plan, explain, and logs without creating state. Verify/publish handlers are not present unless a domain owner supplies them, and handlers still require a separate consent argument. `internal/mcp/server_test.go`, `cmd/autogit/mcp_test.go`. |
| P3-01 | The composition root and output/error lifecycle are in `cmd/autogit/composition.go`; help/presentation, operation services, MCP transport, and the stable command contract are separate files/packages. Existing domain workflows remain behind the existing command functions. |
| P3-02 | `internal/cli/contract.go`, `cmd/autogit/help.go`, `docs/completions/`, `docs/man/autogit.1`, and `docs/quickstart.md` provide command usage, examples, completions, man-page output, version, and clean-machine guidance. |
| P3-03 | `internal/cli/contract.go` defines result/error schema and exit-code classes. `--format human` is opt-in for read-only commands; JSON remains the default. The process entry point writes redacted error envelopes to stderr. `cmd/autogit/output_contract_test.go`. |
| P3-04 | `doctor` now reports runtime, Git version, SQLite health, isolation capabilities, adapter probes, provider mode, and telemetry mode while retaining read-only state inspection. |
| P3-05 | `internal/operations` and `operation_commands.go` add status, explain, resume guidance, cancel, runlog, and bounded undo. Cancel changes only an uncommitted AutoGit intent; undo requires the exact recorded `refs/autogit/commits/<id>` and SHA and never touches a user branch or hosted ref. |
| P3-06 | `internal/install/provenance.go` adds migration previews, atomic provenance, backup discovery, ownership checks, and rollback. `config preview`, `config migrate`, and `config rollback` are explicit and root-scoped. |
| P3-07 | `internal/telemetry` records only allowlisted local JSONL facts when `AUTOGIT_TELEMETRY=local`; default mode is off and has no exporter. The explicit `OTLPHTTPExporter` is injected by callers, bounded, and emits only sanitized attributes. Summary exposes p50/p95/p99 over local durations. |
| P3-08 | `scripts/performance-budgets.json`, `cmd/autogit/performance_test.go`, and `scripts/performance-gate.sh` define/sample p50/p95/p99 budgets for hook latency, 100k-path observation, and CLI startup. CI retains the TSV budget artifact. |
| P3-09 | README, quickstart, man page, completions, trust-boundary, recovery, upgrade, uninstall, and release-runbook links were refreshed. |

## Verification commands

The exact local verification commands for this slice are:

```text
gofmt -w .
go test ./...
go vet ./...
go build ./...
bash -n scripts/*.sh
bash scripts/performance-gate.sh
```

Observed local evidence on 2026-09-07:

- `go test ./...`: pass.
- `go vet ./...`, `go build ./...`, and shell syntax checks: pass.
- `go test -race ./internal/mcp ./internal/operations ./internal/install ./internal/telemetry ./cmd/autogit`: pass.
- `staticcheck@v0.8.1 ./...`: pass.
- `gosec@v2.29.0 ./...`: 0 issues.
- Performance p95: no-candidate hook 4,834,999 ns; 1k-path observation 3,106,321 ns; 100k-path observation 354,914,260 ns; state inspect 2,744,173 ns; verifier boundary 7,077,210 ns; provider push 62,318 ns; CLI startup 78,798 ns. All are below the recorded budgets.

The phase remains NO-GO for alpha until the independent native OS/artifact,
provider, signed-release, and named-review gates in `todo.md` are executed.
