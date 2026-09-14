# Iterative agent review — 2026-09-13

This review followed the Windows path audit. It used two independent reviewers,
then implementation and a second review of the fixes. Findings refer to the
working tree; this is not a claim that the repository has no remaining bugs.

## Reviewer roles

- **Pragmatic Programmer:** Examine explicit contracts, state ownership,
  failure handling, orthogonality, reversibility, and the simplest repair that
  addresses a demonstrated defect. Require a trigger, impact, and regression
  scenario; avoid speculative abstractions or a wholesale rewrite.
- **BDD:** Examine observable behavior using Given/When/Then scenarios. Compare
  dry-run with live behavior, success reports with side effects, recovery with
  interruption, and tests with the behavior their names promise. Prefer
  synthetic fixtures and do not change installed tasks or external remotes.

For another iteration, have both reviewers independently inspect the current
diff and surrounding callers, reconcile duplicate findings, add behavioral
regressions for confirmed bugs, then ask both reviewers to reassess the changes.

## Confirmed findings and repairs

| Finding | Behavior after repair | Regression scenario |
| --- | --- | --- |
| Lock-file deletion allowed different processes to lock different files at the same path | Keep one persistent lock file; release is idempotent | Given a waiting open handle, when the owner releases and the waiter locks, a third run cannot acquire |
| Replacement fallback could move an existing directory aside and replace it with a file | Use the OS replacement operation and propagate its error | Given a directory at the target, writing fails and preserves the directory |
| A `null` state journal passed JSON decoding then panicked during recording | Both journal loaders reject null without modifying it | Given null state, live and read-only loading return a controlled error |
| Empty-input runs ignored report persistence failures | Empty runs use the common persistence/error path | Given an unwritable report target, an empty run reports failure |
| Shared conflicts could be discovered after Archive changed | Check both destinations before writing either | Given a pre-existing Shared conflict, dry-run and live leave Archive unchanged |
| Write flags described destinations as written even when unchanged | Record actual writes separately for each destination | Given a repeat run, both flags are false; repairing only Shared sets only its flag |
| Scheduler status JSON keys did not match Go field tags | Preserve execution results, times, actions, and battery settings | Given synthetic failed-task status, PowerShell JSON round-trips all fields |
| Invalid scheduler arguments could still trigger task actions | Reject malformed flags and trailing arguments first | Given a misspelled remove flag, return usage error without task action |
| Dry-run conflict test did not target the generated filename or assert a conflict | Use the real source hash/date and assert the conflict count | Given an edited generated destination, preview reports one conflict and no write |

Git recovery and scheduler-removal findings received focused follow-up review
and tests. Git publishing is currently rejected by configuration validation;
Git findings concern the retained implementation, not an enabled CLI feature.

## Remaining design work

1. **Dual publication is not a transaction.** Known conflicts are detected before
   writes, but a disk or sync failure between Archive and Shared writes can leave
   one updated copy and an older journal. The report retains completed write
   flags. Durable per-destination progress would be needed for recovery across
   subsequent renderer changes; a two-file atomicity guarantee is not provided.
2. **Git retry needs durable intent before enabling the feature.** The integration
   tracks only archive files written in the current run. A failed preflight or
   commit followed by unchanged output can therefore omit the retry. Track files
   awaiting commit in persistent state and test failure/restart/retry before
   removing the configuration gate.
3. **Verification scope:** synthetic local repositories and PowerShell objects
   establish the tested contracts. They do not establish live Outlook/OneDrive
   behavior or live scheduler installation. Scheduler lifecycle tests are opt-in.

## Validation

Run on Windows 11, Go module `github.com/trustdan/hmba-mail`, working tree as
described above.

| Check | Result |
| --- | --- |
| `go build ./...` | Clean |
| `go vet ./...` | Clean |
| `go test ./...` | All packages pass (`report` has no test files) |
| `go test -race ./...` | All packages pass |
| `gofmt -l .` | Initially flagged 4 files; fixed, now clean |

A follow-up check found a pre-existing defect the reviewers had not covered:
`internal/eml/eml.go`, `internal/eml/eml_test.go`, and `internal/report/report.go`
began with a UTF-8 BOM, and `internal/render/render_test.go` had a trailing blank
line. The BOM is part of the package comment token, so it survives compilation
but corrupts the first line for any tool that reads the file as text. CI ran
`vet`, `test`, and `build` but never `gofmt`, so nothing caught it. The BOMs are
stripped, the file is formatted, and CI now runs a `gofmt` check (gated to Ubuntu,
since `gofmt -l` on a Windows runner can report false positives under autocrlf).

Not exercised by this run:

- The Windows Task Scheduler lifecycle test remains opt-in. It replaces the
  installed `HMBAMailSync` task, so it runs only on a disposable host with
  `HMBA_TEST_SCHEDULER_LIFECYCLE=1` and outside `-short` mode.
- Live Outlook, OneDrive, and remote Git behavior. The suite covers these
  through synthetic fixtures, local repositories, and PowerShell objects only.
