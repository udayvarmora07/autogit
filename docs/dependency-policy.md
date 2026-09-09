# Dependency and workflow policy

Runtime and build dependencies are pinned in `go.mod` and `go.sum`; updates
must be reviewed as code, pass `go mod verify`, and retain a reproducible
build. New dependencies need a concrete complexity or security benefit and a
recorded SPDX license. The release allowlist is Apache-2.0, BSD-2-Clause,
BSD-3-Clause, ISC, MIT, MPL-2.0, and public-domain components. A dependency
outside that set requires maintainer approval before merge.

GitHub Actions are pinned to full commit SHAs. Dependabot proposes Go-module
and action updates weekly; dependency review blocks high-severity additions on
pull requests. CodeQL and OpenSSF Scorecard run with least-privilege workflow
permissions. `scripts/check-dependencies.sh` is the local enforcement point and
is also required by the CI `dependency-policy` job on every push and pull
request.

The direct runtime license inventory is recorded in
[`dependency-licenses.json`](dependency-licenses.json). Release SBOM
generation must include the complete transitive module graph and the exact
source commit.
