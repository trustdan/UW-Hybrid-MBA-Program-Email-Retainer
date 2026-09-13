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
	if code := run([]string{"schedule", "status"}, &out, &errBuf); code != 0 {
		t.Fatalf("schedule status: expected code 0, got %d (err: %s)", code, &errBuf)
	}
	if !strings.Contains(out.String(), "installed") {
		t.Fatalf("schedule status missing 'installed': %s", out.String())
	}
}
