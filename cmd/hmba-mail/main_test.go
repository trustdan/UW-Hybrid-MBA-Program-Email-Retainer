package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustdan/hmba-mail/internal/config"
)

func TestCLIRejectsUnimplementedOperations(t *testing.T) {
	for _, args := range [][]string{{"schedule", "install"}, {"unknown"}, {"check", "--unknown"}} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}

func TestScheduleRejectsInvalidArgumentsBeforeAction(t *testing.T) {
	for _, args := range [][]string{
		{"schedule", "remove", "--confg", "config.json"},
		{"schedule", "remove", "unexpected"},
		{"schedule", "remove", "--config"},
		{"schedule", "run", "--unknown"},
		{"schedule", "status", "unexpected"},
	} {
		var out, stderr bytes.Buffer
		if code := run(args, &out, &stderr); code != 2 || out.Len() != 0 {
			t.Fatalf("%v: expected invalid usage without action output, got code=%d out=%s err=%s", args, code, &out, &stderr)
		}
	}
}

func TestCheckDoesNotCreateDestinations(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults()
	c.Input = "input"
	c.Archive = "archive"
	c.Shared = "shared"
	c.State = "state"
	c.Originals = "originals"
	if err := os.Mkdir(filepath.Join(dir, "input"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"check", "--config", path}, &out, &stderr); code != 0 {
		t.Fatalf("%d: %s", code, &stderr)
	}
	if !strings.Contains(out.String(), "configuration_valid") {
		t.Fatal(out.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatal("check wrote unexpected files")
	}
}

func TestRunAndBackfillDryRunCLI(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults()
	c.Input = "input"
	c.Archive = "archive"
	c.Shared = "input/Markdown"
	c.State = "state"
	c.Originals = "originals"

	_ = os.MkdirAll(filepath.Join(dir, "input"), 0700)
	_ = os.MkdirAll(filepath.Join(dir, "archive"), 0700)
	_ = os.MkdirAll(filepath.Join(dir, "state"), 0700)
	_ = os.MkdirAll(filepath.Join(dir, "originals"), 0700)

	data, _ := json.Marshal(c)
	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, data, 0600)

	// Status with no previous run
	var outStatus, errStatus bytes.Buffer
	if code := run([]string{"status", "--config", cfgPath}, &outStatus, &errStatus); code != 0 {
		t.Fatalf("status exit %d: %s", code, &errStatus)
	}
	if !strings.Contains(outStatus.String(), "no_previous_run") {
		t.Fatalf("unexpected status output: %s", outStatus.String())
	}

	// Backfill dry-run
	var outBackfill, errBackfill bytes.Buffer
	if code := run([]string{"backfill", "--config", cfgPath, "--dry-run"}, &outBackfill, &errBackfill); code != 0 {
		t.Fatalf("backfill exit %d: %s", code, &errBackfill)
	}
	if !strings.Contains(outBackfill.String(), "backfill_complete") {
		t.Fatalf("unexpected backfill output: %s", outBackfill.String())
	}

	// Run dry-run
	var outRun, errRun bytes.Buffer
	if code := run([]string{"run", "--config", cfgPath, "--dry-run"}, &outRun, &errRun); code != 0 {
		t.Fatalf("run exit %d: %s", code, &errRun)
	}
	if !strings.Contains(outRun.String(), "run_complete") {
		t.Fatalf("unexpected run output: %s", outRun.String())
	}
}

func TestScheduleCLI(t *testing.T) {
	// Schedule with no args
	var out, errBuf bytes.Buffer
	if code := run([]string{"schedule"}, &out, &errBuf); code != 2 {
		t.Fatalf("schedule with no args: expected code 2, got %d", code)
	}

	// Schedule unknown
	out.Reset()
	errBuf.Reset()
	if code := run([]string{"schedule", "bogus"}, &out, &errBuf); code != 2 {
		t.Fatalf("schedule bogus: expected code 2, got %d", code)
	}

	// Schedule status
	out.Reset()
	errBuf.Reset()
	code := run([]string{"schedule", "status"}, &out, &errBuf)
	if code != 0 {
		if strings.Contains(errBuf.String(), "service unavailable") || strings.Contains(errBuf.String(), "0x80070003") {
			t.Skipf("skipping: host Task Scheduler service unavailable: %s", errBuf.String())
		}
		t.Fatalf("schedule status: expected code 0, got %d (err: %s)", code, &errBuf)
	}
	if !strings.Contains(out.String(), "installed") {
		t.Fatalf("schedule status missing 'installed': %s", out.String())
	}
}

func TestStatusCorruptLastRunReported(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults()
	c.Input = filepath.Join(dir, "input")
	c.Archive = filepath.Join(dir, "archive")
	c.Shared = filepath.Join(dir, "shared")
	c.State = filepath.Join(dir, "state")
	c.Originals = filepath.Join(dir, "originals")

	_ = os.MkdirAll(c.State, 0700)
	lastRunPath := filepath.Join(c.State, "last-run.json")
	if err := os.WriteFile(lastRunPath, []byte("NOT_JSON{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(c)
	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, data, 0600)

	var out, stderr bytes.Buffer
	code := run([]string{"status", "--config", cfgPath}, &out, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0 for status with corrupt last-run, got %d: %s", code, &stderr)
	}
	if !strings.Contains(out.String(), "corrupt_last_run") {
		t.Fatalf("expected corrupt_last_run in output, got: %s", out.String())
	}
}

func TestRunDryRunDoesNotCreateStateDirCLI(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults()
	c.Input = filepath.Join(dir, "input")
	c.Archive = filepath.Join(dir, "archive")
	c.Shared = filepath.Join(dir, "shared")
	c.State = filepath.Join(dir, "absent-state")
	c.Originals = filepath.Join(dir, "absent-originals")

	_ = os.MkdirAll(c.Input, 0700)
	_ = os.MkdirAll(c.Archive, 0700)
	_ = os.MkdirAll(c.Shared, 0700)

	data, _ := json.Marshal(c)
	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, data, 0600)

	var out, stderr bytes.Buffer
	code := run([]string{"run", "--config", cfgPath, "--dry-run"}, &out, &stderr)
	if code != 0 {
		t.Fatalf("dry-run failed with code %d: %s", code, &stderr)
	}

	// Verify state directory was not created on disk
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatalf("dry-run created state directory: %s", c.State)
	}
}
