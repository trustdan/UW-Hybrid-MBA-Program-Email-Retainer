// Package report defines standard exit codes and JSON schemas for hmba-mail commands.
package report

import "github.com/trustdan/hmba-mail/internal/config"

const (
	// ExitSuccess indicates normal completion or a no-op run with no errors.
	ExitSuccess = 0
	// ExitReportFailure indicates failure writing or encoding the JSON report.
	ExitReportFailure = 1
	// ExitInvalidUsage indicates invalid CLI arguments, unsupported commands, or malformed config.
	ExitInvalidUsage = 2
	// ExitInputUnavailable indicates the input directory does not exist or is unreadable.
	ExitInputUnavailable = 3
	// ExitPartialFailure indicates errors occurred during message conversion or file publication.
	ExitPartialFailure = 4
	// ExitGitBlocked indicates Git operations are blocked by uncommitted work, detached HEAD, or divergence.
	ExitGitBlocked = 5
	// ExitOverlappingRun indicates another instance is currently executing with the active lock.
	ExitOverlappingRun = 6
)

type DiagnosticsReport struct {
	GitAvailable    bool   `json:"git_available"`
	GitVersion      string `json:"git_version,omitempty"`
	TaskInstalled   bool   `json:"task_installed"`
	TaskState       string `json:"task_state,omitempty"`
	InputEMLCount   int    `json:"input_eml_count"`
	ArchiveExists   bool   `json:"archive_exists"`
	SharedExists    bool   `json:"shared_exists"`
	StateExists     bool   `json:"state_exists"`
	OriginalsExists bool   `json:"originals_exists"`
}

type CheckReport struct {
	ReportVersion int                `json:"report_version"`
	Status        string             `json:"status"`
	Config        config.Config      `json:"config"`
	Diagnostics   *DiagnosticsReport `json:"diagnostics,omitempty"`
	Limitations   []string           `json:"limitations"`
}

type RunCounts struct {
	Scanned   int `json:"scanned"`
	Matched   int `json:"matched"`
	Written   int `json:"written"`
	Unchanged int `json:"unchanged"`
	Conflicts int `json:"conflicts"`
	Failed    int `json:"failed"`
}

type MessageRecord struct {
	SourceSHA256   string   `json:"source_sha256"`
	Subject        string   `json:"subject,omitempty"`
	Sender         string   `json:"sender,omitempty"`
	Category       string   `json:"category,omitempty"`
	Date           string   `json:"date,omitempty"`
	Filename       string   `json:"filename,omitempty"`
	ArchiveWritten bool     `json:"archive_written"`
	SharedWritten  bool     `json:"shared_written"`
	Attachments    []string `json:"attachments,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type GitPublishReport struct {
	Enabled   bool   `json:"enabled"`
	Committed string `json:"committed,omitempty"`
	Pushed    bool   `json:"pushed,omitempty"`
	Pending   string `json:"pending,omitempty"`
	Error     string `json:"error,omitempty"`
}

type RunReport struct {
	ReportVersion int               `json:"report_version"`
	Status        string            `json:"status"`
	DryRun        bool              `json:"dry_run"`
	Counts        RunCounts         `json:"counts"`
	Messages      []MessageRecord   `json:"messages"`
	Git           *GitPublishReport `json:"git,omitempty"`
	Errors        []string          `json:"errors,omitempty"`
	Limitations   []string          `json:"limitations,omitempty"`
}

type BackfillFileRecord struct {
	ArchiveRel  string `json:"archive_rel"`
	SharedRel   string `json:"shared_rel"`
	SHA256      string `json:"sha256"`
	Action      string `json:"action"` // "copied", "unchanged", "conflict"
	ConflictMsg string `json:"conflict_msg,omitempty"`
}

type BackfillReport struct {
	ReportVersion int                  `json:"report_version"`
	Status        string               `json:"status"`
	DryRun        bool                 `json:"dry_run"`
	TotalScanned  int                  `json:"total_scanned"`
	Copied        int                  `json:"copied"`
	Unchanged     int                  `json:"unchanged"`
	Conflicts     int                  `json:"conflicts"`
	Files         []BackfillFileRecord `json:"files"`
	Errors        []string             `json:"errors,omitempty"`
}
