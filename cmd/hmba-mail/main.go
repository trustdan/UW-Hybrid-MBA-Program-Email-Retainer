// hmba-mail is the local-file workflow; it never invokes the Graph prototype.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/trustdan/hmba-mail/internal/config"
	"github.com/trustdan/hmba-mail/internal/publish"
	"github.com/trustdan/hmba-mail/internal/report"
	"github.com/trustdan/hmba-mail/internal/scheduler"
)

var version = "dev"

func run(args []string, out, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(out, "hmba-mail: local Go workflow\nCommands: version | check --config PATH | run --config PATH [--dry-run] [--timeout DURATION] | backfill --config PATH [--dry-run] | status --config PATH | schedule <install|status|run|remove>")
		return report.ExitSuccess
	}
	if args[0] == "version" && len(args) == 1 {
		fmt.Fprintln(out, version)
		return report.ExitSuccess
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "check":
		return handleCheck(ctx, args[1:], out, stderr)
	case "run":
		return handleRun(ctx, args[1:], out, stderr)
	case "backfill":
		return handleBackfill(ctx, args[1:], out, stderr)
	case "status":
		return handleStatus(args[1:], out, stderr)
	case "schedule":
		return handleSchedule(ctx, args[1:], out, stderr)
	default:
		fmt.Fprintln(stderr, "unsupported command; use help")
		return report.ExitInvalidUsage
	}
}

func handleCheck(ctx context.Context, args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "configuration JSON path")
	if err := flags.Parse(args); err != nil || *path == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "use check --config PATH")
		return report.ExitInvalidUsage
	}
	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return report.ExitInvalidUsage
	}
	info, err := os.Stat(c.Input)
	if err != nil || !info.IsDir() {
		fmt.Fprintln(stderr, "input directory unavailable")
		return report.ExitInputUnavailable
	}
	entries, err := os.ReadDir(c.Input)
	if err != nil {
		fmt.Fprintln(stderr, "input directory cannot be listed")
		return report.ExitInputUnavailable
	}

	emlCount := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".eml") {
			emlCount++
		}
	}

	diag := &report.DiagnosticsReport{
		InputEMLCount: emlCount,
	}

	if st, err := os.Stat(c.Archive); err == nil && st.IsDir() {
		diag.ArchiveExists = true
	}
	if st, err := os.Stat(c.Shared); err == nil && st.IsDir() {
		diag.SharedExists = true
	}
	if st, err := os.Stat(c.State); err == nil && st.IsDir() {
		diag.StateExists = true
	}
	if st, err := os.Stat(c.Originals); err == nil && st.IsDir() {
		diag.OriginalsExists = true
	}

	if gitPath, err := exec.LookPath("git"); err == nil && gitPath != "" {
		diag.GitAvailable = true
		if vOut, err := exec.CommandContext(ctx, "git", "--version").Output(); err == nil {
			diag.GitVersion = strings.TrimSpace(string(vOut))
		}
	}

	if tStatus, err := scheduler.Status(ctx); err == nil && tStatus != nil {
		diag.TaskInstalled = tStatus.Installed
		diag.TaskState = tStatus.State
	}

	rep := report.CheckReport{
		ReportVersion: 1,
		Status:        "configuration_valid",
		Config:        c,
		Diagnostics:   diag,
		Limitations: []string{
			"Destination writability and OneDrive cloud sync are not verified in check mode",
			"OneDrive EML hydration requires 'Always keep on this device' to prevent cloud-only latency",
		},
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err = enc.Encode(rep); err != nil {
		fmt.Fprintln(stderr, err)
		return report.ExitReportFailure
	}
	return report.ExitSuccess
}

func handleRun(ctx context.Context, args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "configuration JSON path")
	dryRun := flags.Bool("dry-run", false, "simulate conversion and writing without modifying files")
	timeout := flags.Duration("timeout", 0, "maximum run duration (e.g. 5m); defaults to config timeout_seconds")
	if err := flags.Parse(args); err != nil || *path == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "use run --config PATH [--dry-run] [--timeout DURATION]")
		return report.ExitInvalidUsage
	}
	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return report.ExitInvalidUsage
	}

	runTimeout := time.Duration(c.TimeoutSeconds) * time.Second
	if *timeout > 0 {
		runTimeout = *timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	rep, err := publish.Run(runCtx, c, *dryRun)
	if err != nil {
		fmt.Fprintf(stderr, "run failed: %v\n", err)
		if strings.Contains(err.Error(), "run lock active") {
			return report.ExitOverlappingRun
		}
		if strings.Contains(err.Error(), "input directory unavailable") {
			return report.ExitInputUnavailable
		}
		return report.ExitPartialFailure
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err = enc.Encode(rep); err != nil {
		fmt.Fprintln(stderr, err)
		return report.ExitReportFailure
	}
	if rep.Counts.Failed > 0 || rep.Counts.Conflicts > 0 {
		return report.ExitPartialFailure
	}
	return report.ExitSuccess
}

func handleBackfill(ctx context.Context, args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet("backfill", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "configuration JSON path")
	dryRun := flags.Bool("dry-run", false, "simulate backfill without copying files")
	if err := flags.Parse(args); err != nil || *path == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "use backfill --config PATH [--dry-run]")
		return report.ExitInvalidUsage
	}
	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return report.ExitInvalidUsage
	}
	rep, err := publish.Backfill(ctx, c, *dryRun)
	if err != nil {
		fmt.Fprintf(stderr, "backfill failed: %v\n", err)
		return report.ExitPartialFailure
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err = enc.Encode(rep); err != nil {
		fmt.Fprintln(stderr, err)
		return report.ExitReportFailure
	}
	if rep.Conflicts > 0 {
		return report.ExitPartialFailure
	}
	return report.ExitSuccess
}

type StatusLockReport struct {
	Active bool   `json:"active"`
	PID    string `json:"pid,omitempty"`
}

type StatusReport struct {
	ReportVersion int               `json:"report_version"`
	Status        string            `json:"status"`
	Lock          StatusLockReport  `json:"lock"`
	LogsCount     int               `json:"logs_count"`
	LastRun       *report.RunReport `json:"last_run,omitempty"`
}

func handleStatus(args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "configuration JSON path")
	if err := flags.Parse(args); err != nil || *path == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "use status --config PATH")
		return report.ExitInvalidUsage
	}
	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return report.ExitInvalidUsage
	}

	locked, pid, _ := publish.CheckLock(c.State)
	rep := StatusReport{
		ReportVersion: 1,
		Status:        "ok",
		Lock: StatusLockReport{
			Active: locked,
			PID:    pid,
		},
	}

	// Count log files in state/logs
	logsDir := filepath.Join(c.State, "logs")
	if entries, err := os.ReadDir(logsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "run-") && strings.HasSuffix(e.Name(), ".log") {
				rep.LogsCount++
			}
		}
	}

	lastRunPath := filepath.Join(c.State, "last-run.json")
	data, err := os.ReadFile(lastRunPath)
	if os.IsNotExist(err) {
		rep.Status = "no_previous_run"
	} else if err == nil {
		var lastRun report.RunReport
		if err := json.Unmarshal(data, &lastRun); err == nil {
			rep.LastRun = &lastRun
		}
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		fmt.Fprintf(stderr, "encode status: %v\n", err)
		return report.ExitReportFailure
	}
	return report.ExitSuccess
}

func handleSchedule(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "use schedule install --config PATH | schedule status | schedule run | schedule remove [--config PATH]")
		return report.ExitInvalidUsage
	}
	switch args[0] {
	case "install":
		flags := flag.NewFlagSet("schedule install", flag.ContinueOnError)
		flags.SetOutput(stderr)
		path := flags.String("config", "", "configuration JSON path")
		if err := flags.Parse(args[1:]); err != nil || *path == "" || flags.NArg() != 0 {
			fmt.Fprintln(stderr, "use schedule install --config PATH")
			return report.ExitInvalidUsage
		}
		if _, err := config.Load(*path); err != nil {
			fmt.Fprintf(stderr, "invalid config: %v\n", err)
			return report.ExitInvalidUsage
		}
		status, err := scheduler.Install(ctx, *path, "")
		if err != nil {
			fmt.Fprintf(stderr, "schedule install failed: %v\n", err)
			return report.ExitPartialFailure
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(status)
		return report.ExitSuccess

	case "status":
		status, err := scheduler.Status(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "schedule status failed: %v\n", err)
			return report.ExitPartialFailure
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(status)
		return report.ExitSuccess

	case "run":
		if err := scheduler.RunNow(ctx); err != nil {
			fmt.Fprintf(stderr, "schedule run failed: %v\n", err)
			return report.ExitPartialFailure
		}
		fmt.Fprintf(out, "{\n  \"status\": \"task_triggered\",\n  \"task\": %q\n}\n", scheduler.TaskName)
		return report.ExitSuccess

	case "remove":
		flags := flag.NewFlagSet("schedule remove", flag.ContinueOnError)
		flags.SetOutput(stderr)
		path := flags.String("config", "", "configuration JSON path (optional)")
		_ = flags.Parse(args[1:])
		if err := scheduler.Remove(ctx, *path); err != nil {
			fmt.Fprintf(stderr, "schedule remove failed: %v\n", err)
			return report.ExitPartialFailure
		}
		fmt.Fprintf(out, "{\n  \"status\": \"task_removed\",\n  \"task\": %q\n}\n", scheduler.TaskName)
		return report.ExitSuccess

	default:
		fmt.Fprintln(stderr, "unsupported schedule command; use install, status, run, or remove")
		return report.ExitInvalidUsage
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
