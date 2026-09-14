package scheduler

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestLauncherGeneration(t *testing.T) {
	exe := `C:\tools\hmba-mail.exe`
	cfg := `C:\tools\config.json`
	script := GenerateLauncherScript(exe, cfg)

	if !strings.Contains(script, exe) {
		t.Errorf("script missing exe path: %s", script)
	}
	if !strings.Contains(script, cfg) {
		t.Errorf("script missing config path: %s", script)
	}
	if !strings.Contains(script, "WScript.Quit(exitCode)") {
		t.Errorf("script does not propagate exit code: %s", script)
	}
	if !strings.Contains(script, ", 0, True") {
		t.Errorf("script does not hide window and wait: %s", script)
	}
}

func TestWriteLauncher(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "Renée 李 [mail] $data")
	if err := os.Mkdir(tmp, 0700); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(tmp, "fake-exe.exe")
	cfgPath := filepath.Join(tmp, "fake-config.json")

	if err := os.WriteFile(exePath, []byte("fake binary"), 0700); err != nil {
		t.Fatalf("write fake exe: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("{}"), 0600); err != nil {
		t.Fatalf("write fake cfg: %v", err)
	}

	lPath, err := WriteLauncher(exePath, cfgPath)
	if err != nil {
		t.Fatalf("WriteLauncher failed: %v", err)
	}

	expectedPath := filepath.Join(tmp, "run-task.vbs")
	if lPath != expectedPath {
		t.Errorf("expected launcher path %s, got %s", expectedPath, lPath)
	}

	content, err := os.ReadFile(lPath)
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	if len(content) < 2 || len(content)%2 != 0 || binary.LittleEndian.Uint16(content) != 0xfeff {
		t.Fatal("launcher must be UTF-16LE with a BOM")
	}
	units := make([]uint16, (len(content)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(content[2+i*2:])
	}
	script := string(utf16.Decode(units))
	if script != GenerateLauncherScript(exePath, cfgPath) {
		t.Fatalf("launcher paths did not survive encoding: %s", script)
	}
}

func TestStatusNonExistentTask(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skipping Windows Task Scheduler test on non-Windows")
	}

	ctx := context.Background()
	status, err := Status(ctx)
	if err != nil {
		if errors.Is(err, ErrSchedulerUnavailable) || strings.Contains(err.Error(), "service unavailable") {
			t.Skipf("skipping: Windows Task Scheduler service unavailable on this host: %v", err)
		}
		t.Fatalf("Status failed: %v", err)
	}
	// The task may or may not be installed yet; status must return without error
	t.Logf("Task status: installed=%v, state=%s", status.Installed, status.State)
}

func TestWindowsSchedulerLifecycle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skipping Windows Task Scheduler lifecycle test on non-Windows")
	}
	if testing.Short() {
		t.Skip("skipping Windows Task Scheduler lifecycle test in short mode")
	}
	if os.Getenv("HMBA_TEST_SCHEDULER_LIFECYCLE") != "1" {
		t.Skip("set HMBA_TEST_SCHEDULER_LIFECYCLE=1 on a disposable host; this test replaces the HMBAMailSync task")
	}

	ctx := context.Background()
	_, err := Status(ctx)
	if err != nil {
		if errors.Is(err, ErrSchedulerUnavailable) || strings.Contains(err.Error(), "service unavailable") {
			t.Skipf("skipping: Windows Task Scheduler service unavailable on this host: %v", err)
		}
		t.Fatalf("initial Status failed: %v", err)
	}

	tmp := t.TempDir()
	exePath := filepath.Join(tmp, "fake-hmba.exe")
	cfgPath := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(exePath, []byte("fake exe"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}

	instStatus, err := Install(ctx, cfgPath, exePath)
	if err != nil {
		if errors.Is(err, ErrSchedulerUnavailable) || strings.Contains(err.Error(), "Access is denied") || strings.Contains(err.Error(), "service unavailable") {
			t.Skipf("skipping: insufficient permissions or Task Scheduler unavailable: %v", err)
		}
		t.Fatalf("Install failed: %v", err)
	}
	if !instStatus.Installed {
		t.Errorf("expected task to be installed")
	}

	st, err := Status(ctx)
	if err != nil {
		t.Fatalf("Status after install failed: %v", err)
	}
	if !st.Installed {
		t.Errorf("expected status to report installed=true")
	}

	if err := Remove(ctx, cfgPath); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	stAfter, err := Status(ctx)
	if err != nil {
		t.Fatalf("Status after remove failed: %v", err)
	}
	if stAfter.Installed {
		t.Errorf("expected status to report installed=false after removal")
	}
}
