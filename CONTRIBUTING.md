# Contributing to UW Hybrid MBA Program Email Retainer

Thank you for your interest in improving and collaborating on the **UW Hybrid MBA Program Email Retainer** (`hmba-mail`)! This project helps Foster Hybrid MBA students convert course announcements and Canvas notifications into clean, searchable, and locally-retained Markdown.

This guide details how to work with this project, whether you are using it as a template for your own email retention setup, forking it to contribute improvements, opening pull requests, or reporting issues.

---

## Contents

- [Choosing Your Path: Template vs. Fork](#choosing-your-path-template-vs-fork)
- [Using as a Template Repository](#using-as-a-template-repository)
- [Forking the Repository](#forking-the-repository)
- [Pull Request (PR) Workflow](#pull-request-pr-workflow)
- [Using GitHub Issues](#using-github-issues)
- [Privacy and Student Data Protection](#privacy-and-student-data-protection)
- [Development and Testing Standards](#development-and-testing-standards)

---

## Choosing Your Path: Template vs. Fork

| Goal | Recommended Approach | Why? |
| --- | --- | --- |
| **Personal Email Retention** | **Use as Template** | Creates a fresh, independent repository with a clean history. Ideal for tailoring your own configuration, scripts, and personal notes without carrying git commit history. |
| **Contributing Code or Fixes** | **Fork** | Preserves commit history and git linkage, making it seamless to create branches and submit Pull Requests to the upstream repository. |
| **Reporting Bugs or Requesting Features** | **GitHub Issues** | Centralized place to discuss enhancements, track problems, and propose ideas with maintainers. |

---

## Using as a Template Repository

If you want to maintain your own copy of the codebase for personal use, use GitHub's **Template Repository** feature:

1. **Navigate to the Repository on GitHub:**
   Visit [`https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer`](https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer).
2. **Click "Use this template":**
   Click the green **Use this template** button near the top right, then select **Create a new repository**.
3. **Configure Your Repository:**
   - Choose your GitHub account as the owner.
   - Pick a repository name (e.g., `my-hmba-mail` or `UW-Hybrid-MBA-Program-Email-Retainer`).
   - Set visibility to **Private** if you plan to commit custom configuration files, paths, or student notes.
4. **Clone Your New Repository Locally:**
   ```powershell
   git clone https://github.com/<your-username>/<your-repo-name>.git
   cd <your-repo-name>
   ```
5. **Set Up Local Configuration:**
   - Copy `config.example.json` to your local environment (e.g., `$HOME\.hmba-mail\config.json`).
   - Follow the [Install and run](README.md#install-and-run) instructions in `README.md`.

---

## Forking the Repository

If you plan to propose bug fixes, documentation improvements, or new features back to this repository, fork it:

1. **Fork the Repository:**
   Visit [`https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer`](https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer) and click the **Fork** button in the top right.
2. **Clone Your Fork Locally:**
   ```powershell
   git clone https://github.com/<your-username>/UW-Hybrid-MBA-Program-Email-Retainer.git
   cd UW-Hybrid-MBA-Program-Email-Retainer
   ```
3. **Configure the Upstream Remote:**
   Keep your fork synchronized with the original repository by adding it as an `upstream` remote:
   ```powershell
   git remote add upstream https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer.git
   git fetch upstream
   ```
4. **Syncing Upstream Changes:**
   Before starting new work, pull the latest changes from upstream `main`:
   ```powershell
   git checkout main
   git pull upstream main
   git push origin main
   ```

---

## Pull Request (PR) Workflow

Contributions via Pull Requests are warmly welcomed. Follow this process:

### 1. Create a Topic Branch
Never commit directly to your `main` branch. Create a descriptive topic branch:
```powershell
git checkout -b fix/safelink-unwrapping
# or
git checkout -b feature/attachment-summary
```

### 2. Make Your Changes and Test
- Keep changes focused and atomic.
- Verify your changes locally using the PowerShell test script or Go tools:
  ```powershell
  # Run the full test suite and code verification
  powershell -ExecutionPolicy Bypass -File scripts/test.ps1
  ```
  Or using the Go CLI directly:
  ```powershell
  go mod verify
  go vet ./...
  go test -v ./...
  go build -trimpath ./cmd/hmba-mail
  ```
- If adding functionality, include corresponding unit tests in the appropriate package.

### 3. Commit Your Changes
Write clear, concise commit messages following standard conventions:
```powershell
git commit -m "render: handle edge-case nested table layouts in announcements"
```

### 4. Push and Open a Pull Request
1. Push the branch to your GitHub fork:
   ```powershell
   git push -u origin fix/safelink-unwrapping
   ```
2. Navigate to [`https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer`](https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer).
3. GitHub will prompt you to open a Pull Request from your branch.
4. Fill out the PR template:
   - Provide a concise summary of the changes.
   - Reference any related issues (e.g., `Fixes #7` or `Closes #15`).
   - Confirm all tests pass and privacy rules are observed.

### 5. Review & CI Checks
- Automated GitHub Actions CI runs tests on both Windows and Ubuntu runners.
- Maintainers will review the code, suggest adjustments if needed, and merge once approved.

---

## Using GitHub Issues

The repository issue tracker is used to track bugs, suggest features, and discuss architecture.

- **Check Existing Issues First:** Before opening a new issue, search the [Issues tab](https://github.com/trustdan/UW-Hybrid-MBA-Program-Email-Retainer/issues) to see if someone has already reported or discussed the topic.
- **Reporting Bugs:**
  - Select the **Bug Report** template.
  - Detail expected vs. actual behavior.
  - Provide minimal steps to reproduce.
  - Include relevant sanitized command output (`hmba-mail check --config ...`, `hmba-mail status`).
  - Specify operating system, PowerShell version, and `hmba-mail version`.
- **Requesting Enhancements:**
  - Select the **Feature Request** template.
  - Describe the problem you are solving and the proposed solution.

---

## Privacy and Student Data Protection

> [!CAUTION]
> **Academic and Student Data Privacy:**
> Never commit or post real student emails, classmate names, professor contact details, course assignment submissions, grades, or Canvas session tokens in public GitHub issues, discussions, pull requests, or test fixtures.

- When reporting issues involving parsing failures, sanitize or redact the text completely (e.g. replace `jane.doe@uw.edu` with `student@uw.edu`).
- Use the provided synthetic test fixture (`internal/render/testdata/sample.eml`) as a guide for authoring test data.
- Ensure your `.gitignore` continues to exclude local configuration files (`config.json`), runtime data (`journal.json`, `last-run.json`), and email stores.

---

## Development and Testing Standards

- **Go Version:** Go 1.26.0 or newer (as specified in `go.mod`).
- **Formatting:** Format all Go code using `gofmt -s` or `go vet ./...`.
- **Platform Compatibility:** Core email parsing, rendering, and reporting logic should remain cross-platform (tested on Windows and Linux). Windows-specific integrations (Task Scheduler, VBScript) reside in `internal/scheduler` and are stubbed gracefully on non-Windows platforms.
