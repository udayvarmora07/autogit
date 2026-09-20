# Phase 5 native raw-binary install drill

Status: local/native implementation evidence; clean-machine and package-channel
acceptance remains open

## Scope

`scripts/release-install-drill.sh` exercises the smallest supported raw-binary
installation contract in a private temporary root. It accepts two executable
artifacts and proves:

- atomic initial installation into a managed private `bin` directory;
- upgrade to a different version identity;
- checksum rejection of a tampered candidate without replacing the installed
  version;
- downgrade to the previous version;
- rollback preservation when a tampered candidate is rejected; and
- uninstall of only the managed binary.

The evidence output is `autogit.release-install-drill/1`, contains only version
and pass/fail facts, refuses to overwrite an existing output, and records no
paths or source data.

## Local command

```sh
bash scripts/release-install-drill.sh \
  --previous PATH/TO/autogit-linux-amd64 \
  --candidate PATH/TO/autogit-linux-amd64 \
  --output install-drill.json
```

The drill must run on the same native host as the artifacts. It is deliberately
not a package-manager simulation: Homebrew, winget, Scoop, clean-machine
installation, native Windows/macOS execution, and signed-release identity
review remain required before P5-07 is accepted.

## Verification

`scripts/shell_scripts_test.go` builds two versioned Linux artifacts and runs
the full lifecycle drill, including JSON schema facts, checksum protection,
rollback preservation, uninstall, and path-redaction assertions.

The `release` test suite also runs the drill against two freshly built Linux
amd64 candidates, so the release path exercises the same local lifecycle
contract before hosted/native acceptance is considered.

The scheduled/manual native-artifact matrix runs the same drill for all six
supported targets (Linux, macOS, and Windows amd64/arm64) and retains one
redacted JSON evidence artifact per native runner. This validates the raw-binary
lifecycle on each claimed host; package channels, clean-machine installation,
and signed-release identity review remain separate acceptance requirements.

Hosted matrix run `34604368677` passed all six native lifecycle jobs. The
retained evidence files each report schema
`autogit.release-install-drill/1`, `status: "passed"`,
`sensitive_data_recorded: false`, and passed initial-install, upgrade,
tampered-upgrade rejection, downgrade, rollback-preservation, and uninstall
checks for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64/arm64.

## Published-release consumer and Linux evidence — v0.1.2

The published `v0.1.2` bundle at source commit
`abcd3fb3b7ced0b34e33a34bfb335a800806f00b` passed
`scripts/verify-release-artifacts.sh` against the exact repository, tag, and
commit. The verifier accepted every checksum and all required hosted
provenance/SBOM predicates. The six release binaries and evidence files also
passed a direct `sha256sum -c SHA256SUMS` check.

On the native Linux host, the same published candidate passed the complete
install drill against a previous-version binary built from immutable tag
`v0.1.1`: initial install, upgrade, tampered-upgrade rejection, downgrade,
rollback preservation, and uninstall. The redacted result reported
`autogit.release-install-drill/1` and `status: "passed"`.

Package-manager installation on clean native Linux/macOS/Windows machines
remains open; the prior tag was not a GitHub Release, so the previous-version
drill input was built from its immutable source tag.
