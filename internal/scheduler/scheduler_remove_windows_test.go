package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskRemovalReportsFailures(t *testing.T) {
	for _, tt := range []struct {
		name         string
		connectError bool
		hresult      int32
		wantError    bool
	}{
		{name: "removed"},
		{name: "already absent", hresult: -2147024894},
		{name: "access denied", hresult: -2147024891, wantError: true},
		{name: "service failure during delete", hresult: -2147023174, wantError: true},
		{name: "service connection failure", connectError: true, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Override COM creation with fake objects: no real task is accessed.
			script := fmt.Sprintf(`
$script:connectError = $%t
$script:deleteError = %d
$fakeRoot = [PSCustomObject]@{}
$fakeRoot | Add-Member ScriptMethod DeleteTask {
    param($name, $flags)
    if ($script:deleteError -ne 0) {
        throw [System.Runtime.InteropServices.COMException]::new('synthetic delete failure', [int]$script:deleteError)
    }
}
$fakeService = [PSCustomObject]@{}
$fakeService | Add-Member ScriptMethod Connect {
    if ($script:connectError) { throw 'synthetic connection failure' }
}
$fakeService | Add-Member ScriptMethod GetFolder { param($path) return $fakeRoot }
function New-Object { param($ComObject) return $fakeService }
$taskName = 'synthetic-only'
`, tt.connectError, tt.hresult)
			_, err := runPowerShell(context.Background(), script+taskRemovalScript)
			if (err != nil) != tt.wantError {
				t.Fatalf("removal error = %v; want error: %t", err, tt.wantError)
			}
		})
	}
}

func TestRemovePreservesLauncherWhenUnregisterFails(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	launcher := LauncherPath(configPath)
	if err := os.WriteFile(launcher, []byte("launcher"), 0600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("access denied")
	err := removeWith(context.Background(), configPath, func(context.Context) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
	if _, err := os.Stat(launcher); err != nil {
		t.Fatalf("launcher removed after unregister failed: %v", err)
	}
}

func TestRemoveLauncherCleanup(t *testing.T) {
	for _, state := range []string{"present", "absent", "cannot remove"} {
		t.Run(state, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			launcher := LauncherPath(configPath)
			switch state {
			case "present":
				if err := os.WriteFile(launcher, []byte("launcher"), 0600); err != nil {
					t.Fatal(err)
				}
			case "cannot remove":
				// A nonempty directory gives a deterministic removal error.
				if err := os.Mkdir(launcher, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(launcher, "child"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := removeWith(context.Background(), configPath, func(context.Context) error { return nil })
			if (err != nil) != (state == "cannot remove") {
				t.Fatalf("cleanup error: %v", err)
			}
			if state != "cannot remove" {
				if _, err := os.Stat(launcher); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("launcher still present: %v", err)
				}
			}
		})
	}
}
