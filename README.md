# UW Hybrid MBA Program Email Retainer (`hmba-mail`)

Convert locally saved Foster Hybrid MBA announcements and Canvas notifications into searchable Markdown. The Go executable (`hmba-mail`) writes matching notes to a local archive and a shared folder, and preserves the original emails.

**Current scope:** local conversion, Windows automation, and optional automatic Git commit and push. Git publishing defaults to disabled; enable it for an archive stored under `emails/archive/` in a parent repository.

## Contents

- [How it works](#how-it-works)
- [Install and run](#install-and-run)
- [Power Automate setup](#power-automate-setup)
- [Configuration](#configuration)
- [Output and repeat runs](#output-and-repeat-runs)
- [Windows automation](#windows-automation)
- [CLI reference](#cli-reference)
- [Troubleshooting and recovery](#troubleshooting-and-recovery)
- [Template repository setup](#template-repository-setup)
- [Contributing and collaboration](#contributing-and-collaboration)
  - [Forking the repository](#forking-the-repository)
  - [Submitting pull requests](#submitting-pull-requests)
  - [Using GitHub issues](#using-github-issues)
- [Development](#development)

## How it works

```text
Outlook mailbox
  -> Power Automate exports matching messages as .eml
  -> OneDrive syncs HMBA-Emails to your PC
  -> hmba-mail run scans the local input folder
       -> archive/<category>/<filename>.md
       -> shared/<category>/<filename>.md
       -> originals/<full-source-sha256>.eml
       -> state/ (journal, reports, logs)
```

Power Automate and OneDrive handle mailbox access and cloud synchronization separately. The executable does not download mail or require Graph credentials. You can also export `.eml` files manually and use entirely local folders.

The converter unwraps Outlook SafeLinks, simplifies HTML and presentation tables, retains meaningful data tables, and replaces images with text placeholders without fetching them. Markdown includes email metadata and attachment names; attachment contents remain in the original `.eml`.

Both `archive` and `shared` are required, including when sharing only through OneDrive. Conversion completion confirms local writes, not successful OneDrive upload.

## Install and run

### 1. Prerequisites

- Windows with PowerShell for the installation scripts and Task Scheduler integration.
- Go matching [go.mod](go.mod), currently Go 1.26.0 or newer, to build from source. Go is not needed to run the built executable.
- Git to clone this repository, or an extracted source download.
- For the mailbox workflow: an Outlook/Microsoft 365 account, access to the Power Automate connectors, and OneDrive for Business syncing locally.
- For silent scheduled runs: Windows Script Host (`wscript.exe`) and VBScript support must be available.

### 2. Build and install

From PowerShell:

```powershell
git clone https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer.git
cd UW-Hybrid-MBA-Program-Email-Retainer
powershell -ExecutionPolicy Bypass -File scripts/package.ps1
```

> [!NOTE]
> If you previously cloned the repository under its former name (`hmba-mail`), update your local git remote URL:
> ```powershell
> git remote set-url origin https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer.git
> ```

The script runs tests, builds `dist/hmba-mail.exe`, writes `dist/SHA256SUMS`, and installs into `$HOME\.hmba-mail`. It creates a default configuration only if one does not exist. It does not register the scheduled task or add the executable to `PATH`.

For another installation directory or an explicit version label:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/package.ps1 -TargetDir C:\Tools\hmba-mail -Version 0.1.0
```

The build uses the current Go target architecture; the scripts do not force a Windows AMD64 cross-build.

### 3. Edit the configuration

These examples assume the default installation directory. Set these variables again when opening a new PowerShell session:

```powershell
$hmbaExe = Join-Path $HOME '.hmba-mail\bin\hmba-mail.exe'
$hmbaConfig = Join-Path $HOME '.hmba-mail\config.json'
notepad $hmbaConfig
```

Verify all five paths against your actual folders. The installer prioritizes school OneDrive (`OneDriveCommercial`) over personal OneDrive (`OneDriveConsumer`), then falls back to `$HOME\OneDrive - UW`, and accepts an explicit `-OneDrivePath` parameter.

Create the configured input folder if necessary, then put a matching `.eml` in it using the flow below or a manual export. In File Explorer, select **Always keep on this device** for the input folder so mail is available locally. Output directories are created during live processing.

### 4. Check, preview, and convert

```powershell
& $hmbaExe check --config $hmbaConfig
& $hmbaExe run --config $hmbaConfig --dry-run
& $hmbaExe run --config $hmbaConfig
$LASTEXITCODE
& $hmbaExe status --config $hmbaConfig
```

`check` validates configuration and reports diagnostics. Missing output folders before the first run are normal; it does not test destination writability or cloud sync. Its input count includes only top-level `.eml` files, while `run` scans recursively.

A first conversion should report `matched` and `written` greater than zero and create the same note in both destinations. A repeat run with identical input and notes should report `unchanged`. Counts are per message, not per destination. Empty input is a successful no-op and updates `last-run.json`.

**Preview behavior:** `run --dry-run` parses and renders matching mail and inspects destinations to preview would-write, unchanged, and conflict states without writing notes, originals, or state to disk. Reading cloud placeholders can cause OneDrive to download them.

## Power Automate setup

Create this flow in your school account. The repo does not include an importable flow package.

1. Create an automated cloud flow named `Canvas emails`.
2. Select Office 365 Outlook **When a new email arrives (V3)** and the mailbox folder you monitor. Leave From and Subject Filter blank.
3. Add an OR condition: Subject contains `Recent Canvas Notifications`, or Subject contains `Weekly Announcement`.
4. In the yes branch, add **Export email (V2)**. Use the trigger's Message Id.
5. Add OneDrive for Business **Create file** with folder `/HMBA-Emails`, a unique filename ending in `.eml`, and the exported email's binary Body as File Content. Do not use the trigger's HTML message body. A filename expression such as `concat(guid(), '.eml')` avoids using mailbox identifiers as filenames.
6. Save and test with a new matching email. Check flow run history, the cloud `.eml`, and then its local synced copy before running the converter.

Action contracts and connector limitations are documented in Microsoft's [Office 365 Outlook reference](https://learn.microsoft.com/en-us/connectors/office365/) and [OneDrive for Business reference](https://learn.microsoft.com/en-us/connectors/onedriveforbusinessconnector/).

The arrival flow handles new mail. For older mail, export `.eml` files into `input` and run conversion. The CLI's `backfill` command copies existing Markdown; it does not retrieve historical mailbox messages. Keep flow filters and configuration rules aligned when changing subjects.

## Configuration

Use [config.example.json](config.example.json) as the complete starting template. Replace its `<YourUser>` placeholders before use. JSON requires quoted property names, no comments or trailing commas, and doubled backslashes in Windows paths (forward slashes also work).

| Field | Meaning and default |
| --- | --- |
| `version` | Schema version; only `1` is supported. Defaults to `1`. |
| `input` | Required directory containing source `.eml` files; must exist for processing. |
| `archive` | Required local Markdown destination. |
| `shared` | Required second Markdown destination, usually the OneDrive `Markdown` folder. |
| `state` | Required directory for the journal, lock, last-run report, and logs. |
| `originals` | Required directory for raw matching email backups. |
| `rules` | Ordered subject rules. Defaults to the two rules in the example. At least one is required. |
| `max_message_bytes` | Maximum source file size. Default `52428800` (50 MiB); range 1 through 1073741824. |
| `timeout_seconds` | Run timeout. Default `600`; range 1 through 3600. Applies to `run`, not `backfill`. |
| `git.enabled` | Enable scoped automatic commit and push; defaults to `false`. See setup below. |
| `git.parent_repo` | Optional repository root. Use an absolute path; when empty, discovered from `archive`. |
| `git.archive_repo` | Legacy field; unused by the publisher. |
| `git.remote`, `git.branch` | Default to `origin` and `main`; must be nonblank when publishing is enabled. |

Relative paths resolve against the configuration file's directory, not the shell's working directory. Paths do not expand `$HOME`, `%USERPROFILE%`, or `~`; use actual paths. Unknown fields are rejected.

The five directories must not overlap, except that `shared` may be a strict child of `input`. That shared subtree is skipped during scanning. Existing links and junctions are resolved when validating paths.

Rules match case-insensitive subject substrings; the first match wins. Duplicate subjects are rejected, and categories must be either `canvas-digest` or `program-announcement`. Unmatched messages are scanned but produce no notes or original backups.

### Automatic Git publishing

Set `archive` to `<parent-repository>/emails/archive`, and set `git.enabled` to `true`. Optionally set `git.parent_repo` to that repository's absolute path. Git must already have a configured remote, commit identity, and credentials that work without interactive prompts. The example configuration keeps publishing disabled for local-only installations.

Live runs commit newly written archive files and push to the configured remote and branch. The publisher refuses paths outside `emails/archive/`, unrelated staged files, remote divergence, and unpushed commits it did not create. Unstaged unrelated work is preserved. `run --dry-run` does not commit or push.

An interrupted push is recorded in `state/git-journal.json` and retried after checking the pending commit's scope. A preflight failure reports `run_completed_git_blocked`; inspect the report and logs. Local conversion may already have completed. Unchanged files are not staged on later runs, so files written before a blocked commit may require manual staging and publication after resolving the cause. Inspect `git status` and stage only the intended archive files.

## Output and repeat runs

Each Markdown destination has this layout:

```text
canvas-digest/
  2026-09-13-recent-canvas-notifications-<12-character-hash>.md
program-announcement/
  2026-09-13-weekly-announcement-<12-character-hash>.md
```

Filenames use the UTC date from the first usable `Received` header, falling back to `Date`, or `undated` if neither is usable. The subject becomes a lowercase slug, and the suffix is the first 12 characters of the raw email's SHA-256. Identical bytes produce the same filename; a modified export can produce a separate note even if its Message-ID is the same.

Frontmatter contains `title`, `date`, `from`, `sender_address`, `category`, `message_id`, `source_sha256`, `local_source`, and `attachments`. The `local_source` value currently uses a legacy `.local-imports/originals/` path. Locate the actual original using `originals/<source_sha256>.eml` under your configured originals directory.

Existing identical notes are left unchanged. Differing notes are updated only when their source marker and journal hash identify an unedited generated file; otherwise processing reports a conflict. Archive and shared writes happen sequentially, so one destination can succeed before the other fails. The tool does not propagate handwritten edits between destinations or delete old notes when input files disappear.

Keep personal annotations in separate files. Share the configured `shared` folder with intended readers; sharing its parent input folder also exposes raw emails. Notes retain sender information and links, and originals retain full message contents and attachments.

### Copy an existing Markdown archive

```powershell
& $hmbaExe backfill --config $hmbaConfig --dry-run
& $hmbaExe backfill --config $hmbaConfig
```

Backfill recursively copies **all `.md` files** under `archive` to matching relative paths under `shared`, including handwritten notes and README files. It needs no source `.eml`, skips identical destinations, and reports differing destinations as conflicts. It may add a shared-folder README when files were copied and no README exists. Backfill currently does not acquire the run lock; execute it while conversion is idle. Inspect the JSON `errors` array as well as the exit code.

## Windows automation

After a successful manual run, install the task using the executable in its permanent location:

```powershell
& $hmbaExe schedule install --config $hmbaConfig
& $hmbaExe schedule status
& $hmbaExe schedule run
```

The task is named `HMBAMailSync`. Installation creates or replaces that task and writes `run-task.vbs` beside the config. It runs as the current signed-in user, silently through `wscript.exe`, two minutes after logon. Battery execution is allowed, overlapping task instances are ignored, and Task Scheduler imposes a 15-minute execution limit in addition to the application's timeout.

**The installed trigger is logon-only.** There is no continuous watcher or recurring polling interval. Mail arriving later waits for another manual or scheduled invocation. `schedule run` starts the background task and returns before conversion completes; inspect `status` and the logs afterward.

For hourly processing while signed in, add a second trigger after installation. This preserves the logon trigger and adds daily 07:00 with hourly repetition for 24 hours, matching the production setup restored on 2026-09-15:

```powershell
$hmbaTaskService = New-Object -ComObject 'Schedule.Service'
$hmbaTaskService.Connect()
$hmbaTaskFolder = $hmbaTaskService.GetFolder('\')
$hmbaTaskDefinition = $hmbaTaskFolder.GetTask('HMBAMailSync').Definition
# Avoid duplicating the same recurring trigger when rerunning this setup.
$hmbaHourly = @($hmbaTaskDefinition.Triggers | Where-Object {
    $_.Type -eq 2 -and $_.Repetition.Interval -eq 'PT1H'
})
if ($hmbaHourly.Count -eq 0) {
    $hmbaTrigger = $hmbaTaskDefinition.Triggers.Create(2) # Daily
    $hmbaTrigger.StartBoundary = (Get-Date).Date.AddHours(7).ToString('yyyy-MM-ddTHH:mm:ss')
    $hmbaTrigger.DaysInterval = 1
    $hmbaTrigger.Repetition.Interval = 'PT1H'
    $hmbaTrigger.Repetition.Duration = 'P1D'
    $hmbaTrigger.Enabled = $true
    $hmbaTaskDefinition.Settings.StartWhenAvailable = $true
    $hmbaTaskFolder.RegisterTaskDefinition('HMBAMailSync', $hmbaTaskDefinition, 6, $null, $null, 3) | Out-Null
}
```

`schedule install` replaces the task, removing this additional trigger; repeat the setup afterward. `schedule status` currently reads only the first trigger. Inspect all triggers directly:

```powershell
(Get-ScheduledTask -TaskName HMBAMailSync).Triggers |
    Select-Object CimClass, StartBoundary, Delay, Repetition
Get-ScheduledTaskInfo -TaskName HMBAMailSync
```

If `schedule status` reports `installed: false`, register the task first. Packaging alone does not register it. Confirm a background run completes with exit code 0 and a fresh log; a successful launch request alone does not confirm conversion or Git publication.

To disable automation and remove its launcher:

```powershell
& $hmbaExe schedule remove --config $hmbaConfig
```

This preserves your executable, configuration, notes, originals, and state. Omitting `--config` removes only the task. For upgrades, wait for active conversion to finish and rerun `package.ps1` with the same target directory; existing configuration is preserved. Reinstall the task if the executable or config location changes.

## CLI reference

Use the full executable path or `$hmbaExe` from the quickstart; the installer does not change `PATH`.

| Command | Purpose |
| --- | --- |
| `help` or `--help` | List supported commands. |
| `version` | Print the build version (`dev` for a plain Go build). |
| `check --config PATH` | Validate config and report local diagnostics as JSON. |
| `run --config PATH [--dry-run] [--timeout DURATION]` | Convert local mail; a positive timeout such as `5m` overrides the config timeout. |
| `backfill --config PATH [--dry-run]` | Copy existing archive Markdown into shared. |
| `status --config PATH` | Show lock status, log count, and saved last-run report. |
| `schedule install --config PATH` | Register the Windows task and launcher. |
| `schedule status` | Query task registration and execution status. |
| `schedule run` | Request a background run of the installed task. |
| `schedule remove [--config PATH]` | Remove the task and optionally its launcher. |

Reports go to stdout; command errors go to stderr. Inspect `$LASTEXITCODE` immediately after invocation.

| Exit | Meaning |
| --- | --- |
| `0` | Success or no changes required. Review report errors too; see known gaps below. |
| `1` | Failure encoding or writing a report. |
| `2` | Invalid arguments or configuration. |
| `3` | Input directory unavailable (`check` or `run`). |
| `4` | Conversion, publication, backfill, or scheduler failure/conflict. |
| `5` | Reserved for Git publication blocking; not currently emitted by the CLI. |
| `6` | A live conversion already holds the state-directory run lock. |

## Troubleshooting and recovery

| Symptom | What to inspect or do |
| --- | --- |
| Input unavailable | Verify `input`, the OneDrive account, local sync, and folder permissions. `run` waits up to 10 seconds for the directory. |
| Scanned is zero | Check the flow's run history and confirm local files end in `.eml`. The shared subtree is excluded. |
| Scanned is positive, matched is zero | Compare email Subject headers with configured substring rules. |
| Message fails to parse | Confirm the flow saved the exported email rather than HTML body text. Inspect the per-message error or source file locally. |
| Message exceeds size limit | Inspect its size and adjust `max_message_bytes` within the allowed range if appropriate. |
| Notes missing online | Verify local shared output first, then inspect OneDrive sync status and sharing permissions. |
| Destination conflict | Preserve the edited file outside the generated destination, then move it out of the way and rerun to regenerate. There is no force-overwrite option. |
| Overlapping run | Wait for the active run; check `status`. A leftover lock file alone is not proof of an active lock. |
| Background task does nothing | Check `schedule status`, Task Scheduler history, executable/config locations, and Windows Script Host availability. Run the CLI directly to see errors. |
| Status looks stale | Empty input refreshes `last-run.json`; early failures and dry runs do not. Inspect recent logs and direct command output. |

Runtime files live under the configured `state` directory:

- `journal.json` records source and generated hashes used to distinguish generated content from manual edits.
- `last-run.json` contains a saved conversion report, including message metadata.
- `logs/run-*.log` contains operational logs, with rotation targeting 15 files. Sanitization masks selected token-like query values; paths and subject-derived filenames can remain.

Back up configuration, the journal, originals, and edited notes before moving an installation or recovering from a failure. Do not delete the journal as a routine reset: losing generated hashes can turn future updates into conflicts. Sources are not automatically removed or rotated, so input and original storage grow over time.

See [repository review](docs/repository-review.md) for architecture, operating workflow inspection, and remediation details.

## Template repository setup

This repository can serve as a template for students setting up their own personal email retention pipeline:

1. **Create your personal repository**:
   - Navigate to [UW-Hybrid-MBA-Program-Email-Retainer](https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer) on GitHub.
   - Click the green **Use this template** button (near top right) and choose **Create a new repository**.
   - **Recommended Visibility:** Select **Private** if you intend to store custom configuration paths, personal notes, or personalized scripts in your repository.
2. **Clone your repository**:
   ```powershell
   git clone https://github.com/<your-username>/<your-repo-name>.git
   cd <your-repo-name>
   ```
3. **Build and configure**:
   - Run `powershell -ExecutionPolicy Bypass -File scripts/package.ps1` to build the executable into `$HOME\.hmba-mail`.
   - Configure `$HOME\.hmba-mail\config.json` with your personal UW OneDrive folder paths.
   - Starting from a template gives you a clean git history without carrying upstream project commits, making it ideal for personal daily usage.

## Contributing and collaboration

We welcome suggestions, bug fixes, and contributions from students and developers. See [CONTRIBUTING.md](CONTRIBUTING.md) for full contribution guidelines.

### Forking the repository

If you plan to contribute bug fixes or improvements back to the main project:

1. Click **Fork** on the [UW-Hybrid-MBA-Program-Email-Retainer](https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer) page.
2. Clone your fork locally:
   ```powershell
   git clone https://github.com/<your-username>/UW-Hybrid-MBA-Program-Email-Retainer.git
   cd UW-Hybrid-MBA-Program-Email-Retainer
   ```
3. Add the upstream remote to keep your fork updated:
   ```powershell
   git remote add upstream https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer.git
   git fetch upstream
   ```
4. Keep your `main` branch synced with upstream:
   ```powershell
   git checkout main
   git pull upstream main
   git push origin main
   ```

### Submitting pull requests

1. **Branch:** Create a dedicated branch for your change:
   ```powershell
   git checkout -b feature/your-feature-name
   ```
2. **Verify:** Run tests locally before opening a pull request:
   ```powershell
   powershell -ExecutionPolicy Bypass -File scripts/test.ps1
   ```
3. **Commit & Push:** Commit your changes with clear descriptions and push the branch to your fork.
4. **Open PR:** On GitHub, open a Pull Request targeting `main` on `trustdan/UW-Hybrid-MBA-Program-Email-Retainer`. Complete the PR template checklist.
5. **CI Checks:** Automated GitHub Actions workflows will run tests and build checks across Windows and Ubuntu.

### Using GitHub issues

Track bugs, ask questions, or propose features via [GitHub Issues](https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer/issues):

- **Bug Reports:** Use the bug report template. Provide sanitized output from `hmba-mail check` or `hmba-mail status`, reproduction steps, and OS/Go versions.
- **Feature Requests:** Use the feature request template to outline problems and propose improvements.
- **Privacy Notice:** Never include real student emails, classmate names, grades, or private cohort messages in issues or PRs. Use synthetic or redacted data (e.g. `student@uw.edu`).

## Development

```powershell
# Vet and run the test suite
powershell -ExecutionPolicy Bypass -File scripts/test.ps1

# Build without installing or registering a task
powershell -ExecutionPolicy Bypass -File scripts/build.ps1 -Version 0.1.0
```

Equivalent core checks are `go mod verify`, `go vet ./...`, `go test ./...`, and `go build ./cmd/hmba-mail`. [CI](.github/workflows/ci.yml) runs verification, vet, tests, and a build on Windows and Ubuntu, and automatically bundles release archives and SHA256 checksums on version tags (`v*`). Scheduler integration is Windows-specific; the non-Windows implementation reports it as unsupported. Unit tests do not establish that a live Outlook/OneDrive flow or registered Windows task works.

The scheduler lifecycle test is opt-in because it replaces and removes the `HMBAMailSync` task. On a disposable Windows host, set `$env:HMBA_TEST_SCHEDULER_LIFECYCLE = '1'` before running `go test ./internal/scheduler -run TestWindowsSchedulerLifecycle -count=1`. Ordinary test runs exercise launcher generation and PowerShell path handling without registering a task.

| Path | Responsibility |
| --- | --- |
| `cmd/hmba-mail` | CLI parsing, diagnostics, reports, and exit codes. |
| `internal/config` | Configuration validation, path resolution, subject rules. |
| `internal/eml`, `internal/render`, `internal/naming` | MIME parsing, Markdown generation, deterministic filenames. |
| `internal/publish` | Discovery, dual writes, backfill, originals, journal, locks, logs. |
| `internal/scheduler` | Windows task registration and launcher. |
| `internal/gitpub` | Git implementation currently gated off by config validation. |
| `internal/report` | JSON report types and exit-code constants. |
| `scripts` | Build, test, and local installation scripts. |

For changes, run the checks above and describe relevant manual validation. Use synthetic or redacted emails in tests and bug reports. Do not commit real mail, local configuration, generated notes, or runtime reports. Releases bundle the standalone Windows executable, example configuration, instructions, and checksums.


## License

MIT License. Copyright (c) 2026 Daniel Rust. See [LICENSE](LICENSE).
