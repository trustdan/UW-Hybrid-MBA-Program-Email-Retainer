# HMBA Mail Automation (`hmba-mail`)

A lightweight, standalone Go automation engine that turns incoming Foster Hybrid MBA email announcements and Canvas notifications into clean, deterministic Markdown notes.

Designed for classmates who want automated, searchable class notes in **OneDrive** or **Git** without requiring Python, WSL, or complex Graph API developer tenant consent.

---

## How It Works

```text
UW Outlook (Exchange Online)
       │ (incoming Canvas notifications & Weekly Announcements)
       ▼
Power Automate Cloud Flow: "Canvas emails"
       │ (exports raw .eml to OneDrive)
       ▼
OneDrive for Business: "HMBA-Emails/<Message-Id>.eml"
       │ (synced locally to your PC)
       ▼
Windows Task Scheduler: "HMBAMailSync" (Logon trigger + 2-min delay)
       │ (launches hmba-mail.exe silently in the background)
       ├────────────────────────────────────────┬────────────────────────────────────────┐
       ▼                                        ▼                                        ▼
Local Archive / Git:                    OneDrive Shared Notes:                 Originals Backup:
Documents/HMBA-Archive/                 OneDrive/HMBA-Emails/Markdown/         .hmba-mail/originals/
(Searchable Markdown notes)             (Classmate-facing shared folder)       (Byte-for-byte raw EML)
```

---

## Key Features

- **Zero-Dependency Runtime**: Compiles into a single, standalone Windows 64-bit `.exe` with zero external runtime dependencies.
- **Deterministic Markdown**:
  - Unwraps Outlook Safelinks back to clean canonical URLs.
  - Flattens complex Canvas presentation tables while preserving meaningful data tables.
  - Strips invisible 1x1 tracking pixels and inline styling.
  - Converts images into descriptive alt-text placeholders (never fetches external images for privacy).
  - Preserves sender, dates, subject, and attachment names in standard YAML frontmatter.
- **Dual-Destination Publishing**: Converted notes are saved both to a local archive (or Git repository) and to a classmate-facing OneDrive folder (`Markdown/`).
- **Silent Background Execution**: Installs directly into Windows Task Scheduler with a logon trigger and windowless VBScript launcher. No pop-ups or console windows.
- **Fail-Safe & Idempotent**:
  - OS-backed kernel `LockFileEx` non-blocking exclusive locking prevents overlapping runs.
  - Detects manual user edits and prevents overwrite conflicts.
  - Prunes nested output directories during discovery so generated Markdown is never re-scanned as input mail.
  - Backs up raw source emails byte-for-byte by full SHA-256 hash.

---

## Modes of Operation

### Mode A: OneDrive-Only Mode (Recommended for Classmates)
No Git repository required! Notes are automatically converted and saved to `OneDrive - UW/HMBA-Emails/Markdown/`, which you can share directly with classmates or open in Obsidian/VS Code/Notepad.

### Mode B: Git Archive Mode
Automatically stages, commits, and pushes converted Markdown notes to a private or shared Git repository (e.g. GitHub) with preflight safety checks and interrupted push recovery.

---

## Quickstart & Installation

### 1. Prerequisites
- Windows 10 or 11.
- Microsoft OneDrive for Business (connected to your UW account).
- [Go 1.26+](https://go.dev/dl/) (only required if building from source).

### 2. Build & Package
Clone this repository and run the automated packaging script from PowerShell:

```powershell
git clone https://github.com/trustdan/hmba-mail.git
cd hmba-mail
powershell -ExecutionPolicy Bypass -File scripts/package.ps1
```

This installs the binary and config into your user profile at `$HOME\.hmba-mail`:
- `bin/hmba-mail.exe`: Standalone binary.
- `config.json`: Active configuration.
- `state/`: Execution logs and journal.
- `originals/`: Local byte-preserved EML originals.

### 3. Verify Diagnostics
Test the installation from any directory:

```powershell
& "$HOME\.hmba-mail\bin\hmba-mail.exe" check --config "$HOME\.hmba-mail\config.json"
```

---

## Power Automate Cloud Flow Setup

To have Microsoft 365 automatically export class emails into your OneDrive:

1. Sign in to [Power Automate](https://make.powerautomate.com/) using your UW account.
2. Click **Create** → **Automated cloud flow**.
3. Name your flow: `Canvas emails`.
4. Choose the trigger: **When a new email arrives (V3)** (Office 365 Outlook connector).
   - Leave **From** and **Subject Filter** blank.
5. Add a **Condition**:
   - Set condition logic to **OR**:
     - `Subject` contains `Recent Canvas Notifications`
     - `Subject` contains `Weekly Announcement`
6. Under **If yes**, add action: **Export email (V2)**:
   - Message Id: select dynamic content **Message Id** from the trigger.
7. Add action: **Create file** (OneDrive for Business connector):
   - **Folder Path**: `/HMBA-Emails`
   - **File Name**: dynamic content `Message Id` + `.eml` (e.g. `@{triggerOutputs()?['body/id']}.eml`)
   - **File Content**: dynamic content `Body` from the *Export email (V2)* step.
8. Save the flow.

> [!TIP]
> **Important OneDrive Tip**:
> In Windows File Explorer, navigate to your `OneDrive - UW` directory, right-click the `HMBA-Emails` folder, and select **"Always keep on this device"**. This prevents OneDrive Files On-Demand from dehydrating `.eml` files to the cloud, ensuring instant local access.

---

## Windows Task Scheduler Automation

Register the background logon task to run automatically whenever you sign in to your PC:

```powershell
& "$HOME\.hmba-mail\bin\hmba-mail.exe" schedule install --config "$HOME\.hmba-mail\config.json"
```

- **Trigger**: Runs 2 minutes after you log on to Windows (giving OneDrive time to start and sync new files).
- **Execution**: Windowless and silent (`wscript.exe //B //Nologo run-task.vbs`).
- **Battery**: Allowed to run on battery power.
- **Single Instance**: Prevents duplicate overlapping runs.

### Task Management Commands

```powershell
# Check task status
& "$HOME\.hmba-mail\bin\hmba-mail.exe" schedule status

# Trigger an immediate run in the background
& "$HOME\.hmba-mail\bin\hmba-mail.exe" schedule run

# Remove the scheduled task
& "$HOME\.hmba-mail\bin\hmba-mail.exe" schedule remove
```

---

## CLI Reference

```powershell
# Check configuration and environment diagnostics
hmba-mail.exe check --config PATH

# Run manual conversion (scans input, outputs Markdown to archive & shared folder)
hmba-mail.exe run --config PATH

# Dry-run conversion (previews matching files without writing to disk or Git)
hmba-mail.exe run --config PATH --dry-run

# Historical backfill (copies existing Markdown notes to OneDrive without needing raw EMLs)
hmba-mail.exe backfill --config PATH

# Inspect last run counts and status
hmba-mail.exe status --config PATH

# Print version
hmba-mail.exe version
```

### Exit Codes
- `0`: Success / no changes needed
- `1`: Report output failure
- `2`: Invalid configuration or CLI arguments
- `3`: Input directory unavailable or unreadable
- `4`: Partial conversion or user conflict detected
- `5`: Git publication blocked
- `6`: Overlapping run in progress (`LockFileEx` active)

---

## Configuration Reference (`config.json`)

```json
{
  "version": 1,
  "input": "C:\\Users\\<User>\\OneDrive - UW\\HMBA-Emails",
  "archive": "C:\\Users\\<User>\\Documents\\HMBA-Archive",
  "shared": "C:\\Users\\<User>\\OneDrive - UW\\HMBA-Emails\\Markdown",
  "state": "C:\\Users\\<User>\\.hmba-mail\\state",
  "originals": "C:\\Users\\<User>\\.hmba-mail\\originals",
  "rules": [
    { "subject": "Recent Canvas Notifications", "category": "canvas-digest" },
    { "subject": "Weekly Announcement", "category": "program-announcement" }
  ],
  "max_message_bytes": 52428800,
  "timeout_seconds": 600,
  "git": {
    "enabled": false,
    "archive_repo": "",
    "remote": "origin",
    "branch": "main"
  }
}
```

---

## License

MIT License. Copyright (c) 2026 Daniel Rust. See [LICENSE](LICENSE) for details.
