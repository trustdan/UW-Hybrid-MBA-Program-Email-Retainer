# Repository review (2026-09-13)

Scope: README accuracy, installation and operating workflows, implementation inspection, test suite coverage, and pre-push quality hardening.

## Summary of remediations (pre-push update)

All five highest-priority follow-ups, key behavior/onboarding gaps, and maintenance gaps have been addressed and verified with automated tests.

| Gap | Status | Resolution and verification |
| --- | --- | --- |
| Persistence failures can report success | **Resolved** | [LoadJournal](../internal/publish/state.go) distinguishes missing state from unreadable/corrupt files and returns explicit errors. [Run](../internal/publish/publish.go) checks errors from `journal.Save()` and `last-run.json` persistence, recording errors and returning non-zero exit codes. Verified in `TestLoadJournal_CorruptJSON` and `TestRunCorruptJournalReturnsError`. |
| Backfill can race conversion | **Resolved** | [Backfill](../internal/publish/publish.go) now acquires the state directory run lock when not in dry-run mode. Verified in `TestBackfillLockPreventsConcurrentRunWithLive`. |
| Backfill read errors can return exit 0 | **Resolved** | [handleBackfill](../cmd/hmba-mail/main.go) checks `len(rep.Errors) > 0` and returns exit code 4 (`ExitPartialFailure`). Read errors record `Action: "error"` with status `backfill_failed`. |
| Dry-run is not fully read-only or a conflict preview | **Resolved** | `LoadJournalReadOnly` loads state without creating directories or files on disk. `Run` in dry-run mode previews destination states via `PreviewMarkdownDestination`, accurately identifying would-write, unchanged, and user-edited conflict states without disk mutation. Verified in `TestRunDryRunDoesNotCreateStateDirAndPreviewsConflicts` and `TestRunDryRunDoesNotCreateStateDirCLI`. |
| Scheduler tests depend on the host | **Resolved** | Task Scheduler COM failures (such as `0x80070003` or disabled services in CI/sandboxes) are wrapped with `ErrSchedulerUnavailable`. Unit tests skip gracefully when the service is unavailable on the host. Added `TestWindowsSchedulerLifecycle` to validate install, status, and removal on supported hosts. |

## Behavior and onboarding gaps

- **Git publishing is inaccessible through the CLI.** Retained gate: [config validation](../internal/config/config.go) continues to reject `git.enabled: true` pending dedicated implementation review. Documented in README.
- **School OneDrive is not the installer's first choice.** [package.ps1](../scripts/package.ps1) now prioritizes `OneDriveCommercial` over `OneDriveConsumer`, checks `OneDrive`, and accepts an optional `-OneDrivePath` parameter.
- **Original references use a legacy location.** [Render](../internal/render/render.go) now defaults to portable provenance (`originals/<hash>.eml`) instead of hardcoding `.local-imports/originals/`, and supports custom source references via `RenderWithSource`. Verified in `TestRealEMLConversionFidelity` and `TestRenderCustomProvenanceSource`.
- **Status can be stale or misleading.** Empty-input runs now persist `last-run.json`. Final issue status is assigned prior to report serialization so saved reports match returned reports. [handleStatus](../cmd/hmba-mail/main.go) detects and reports `corrupt_last_run` and `unreadable_last_run`. Verified in `TestRunEmptyInputSavesLastRun` and `TestStatusCorruptLastRunReported`.
- **Backfill copies all Markdown.** Documented in README as expected behavior for full archive backfill.

## Distribution and maintenance gaps

- **Synthetic test fixture for CI fidelity:** Replaced external private dependency in `TestRealEMLConversionFidelity` with in-tree redacted fixture [sample.eml](../internal/render/testdata/sample.eml). Test now executes and passes in all environments without skipping.
- **Gitignore cleanliness:** [.gitignore](../.gitignore) updated to exclude root `config.json`, runtime reports (`last-run.json`, `journal.json`, `*.tmp`), and release bundles (`release-pkg/`, `*.zip`).
- **Release workflow:** Automated GitHub Releases packaging workflow is configured in `.github/workflows/ci.yml`.

## Validation results

- Go 1.26.5 on Windows AMD64.
- `go vet ./...` passed via `scripts/test.ps1`.
- `go test -v ./...` passed across all packages (`cmd/hmba-mail`, `internal/config`, `internal/eml`, `internal/gitpub`, `internal/naming`, `internal/publish`, `internal/render`, `internal/scheduler`).
- `go build -trimpath ./cmd/hmba-mail` succeeded with clean output.
